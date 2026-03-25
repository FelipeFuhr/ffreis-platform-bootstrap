package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
	"github.com/ffreis/platform-bootstrap/internal/logging"
)

// ExpectedResource describes a single AWS resource that bootstrap manages.
type ExpectedResource struct {
	ResourceType string
	ResourceName string
}

// ExpectedResources returns the complete set of platform resources that
// bootstrap creates for the given config. Both Run and audit use this list as
// the authoritative source of what is platform-managed, so adding a new step
// only requires updating this function.
func ExpectedResources(cfg *config.Config) []ExpectedResource {
	return []ExpectedResource{
		{"DynamoDBTable", cfg.RegistryTableName()},
		{"S3Bucket", cfg.StateBucketName()},
		{"DynamoDBTable", cfg.LockTableName()},
		{"IAMRole", config.RoleNamePlatformAdmin},
		{"SNSTopic", cfg.EventsTopicName()},
		{"AWSBudget", cfg.BudgetName()},
	}
}

type bootstrapRunner struct {
	cfg   *config.Config
	c     *platformaws.Clients
	log   *slog.Logger
	tags  map[string]string
	topic string
}

func newBootstrapRunner(ctx context.Context, cfg *config.Config, clients *platformaws.Clients) *bootstrapRunner {
	return &bootstrapRunner{
		cfg:  cfg,
		c:    clients,
		log:  logging.FromContext(ctx),
		tags: platformaws.RequiredTags(cfg.OrgName),
	}
}

func (r *bootstrapRunner) tryPublish(ctx context.Context, e platformaws.Event) {
	if r.topic == "" {
		return // topic not yet created; skip silently
	}
	if err := platformaws.PublishEvent(ctx, r.c.SNS, r.topic, e); err != nil {
		r.log.Warn("SNS publish failed, continuing",
			"event_type", e.EventType,
			"resource_type", e.ResourceType,
			"resource_name", e.ResourceName,
			"error", err,
		)
	}
}

func (r *bootstrapRunner) tryRegister(ctx context.Context, resourceType, resourceName string) {
	rec, err := platformaws.NewRegistryRecord(resourceType, resourceName, r.c.CallerARN, r.tags)
	if err != nil {
		r.log.Warn("failed to build registry record, continuing",
			"resource_type", resourceType,
			"resource_name", resourceName,
			"error", err,
		)
		return
	}
	if err := platformaws.RegisterResource(ctx, r.c.DynamoDB, r.cfg.RegistryTableName(), rec); err != nil {
		r.log.Warn("registry write failed, continuing",
			"resource_type", resourceType,
			"resource_name", resourceName,
			"error", err,
		)
	}
}

func (r *bootstrapRunner) postEnsure(ctx context.Context, resourceType, resourceName string, existed bool) {
	eventType := platformaws.EventTypeResourceCreated
	if existed {
		eventType = platformaws.EventTypeResourceExists
	}

	r.tryPublish(ctx, platformaws.NewEvent(eventType, resourceType, resourceName, r.c.CallerARN))
	r.tryRegister(ctx, resourceType, resourceName)
}

func (r *bootstrapRunner) ensureResource(ctx context.Context, resourceType, resourceName string, existed bool, ensureFn func(context.Context) error) error {
	if err := ensureFn(ctx); err != nil {
		return err
	}
	r.postEnsure(ctx, resourceType, resourceName, existed)
	return nil
}

func (r *bootstrapRunner) steps() []step {
	cfg := r.cfg

	return []step{
		{
			name: "registry-table",
			desc: fmt.Sprintf("ensure DynamoDB registry table %s (PAY_PER_REQUEST, PK=PK, SK=SK)", cfg.RegistryTableName()),
			run: func(ctx context.Context) error {
				return r.ensureResource(ctx, "DynamoDBTable", cfg.RegistryTableName(),
					r.c.TableExists(ctx, cfg.RegistryTableName()),
					func(ctx context.Context) error {
						return platformaws.EnsureRegistryTable(ctx, r.c.DynamoDB, cfg.RegistryTableName(), r.tags)
					},
				)
			},
		},
		{
			name: "account-config",
			desc: fmt.Sprintf("write account and admin configuration to registry table %s", cfg.RegistryTableName()),
			run: func(ctx context.Context) error {
				for name, email := range cfg.Accounts {
					if err := platformaws.WriteConfig(
						ctx, r.c.DynamoDB, cfg.RegistryTableName(),
						"account", name, r.c.CallerARN,
						map[string]string{"email": email},
					); err != nil {
						return fmt.Errorf("writing account config %q: %w", name, err)
					}
					r.log.Info("account config written", "step", "account-config", "account", name)
				}
				if cfg.AdminEmail != "" {
					if err := platformaws.WriteConfig(
						ctx, r.c.DynamoDB, cfg.RegistryTableName(),
						"admin", "alert_email", r.c.CallerARN,
						map[string]string{"email": cfg.AdminEmail},
					); err != nil {
						return fmt.Errorf("writing admin alert email config: %w", err)
					}
					r.log.Info("admin config written", "step", "account-config", "key", "alert_email")
				}
				return nil
			},
		},
		{
			name: "state-bucket",
			desc: fmt.Sprintf("ensure S3 state bucket %s (versioning on, public access blocked)", cfg.StateBucketName()),
			run: func(ctx context.Context) error {
				return r.ensureResource(ctx, "S3Bucket", cfg.StateBucketName(),
					r.c.BucketExists(ctx, cfg.StateBucketName()),
					func(ctx context.Context) error {
						return platformaws.EnsureStateBucket(ctx, r.c.S3, cfg.StateBucketName(), cfg.StateRegion, r.tags)
					},
				)
			},
		},
		{
			name: "lock-table",
			desc: fmt.Sprintf("ensure DynamoDB lock table %s (PAY_PER_REQUEST, PK=LockID)", cfg.LockTableName()),
			run: func(ctx context.Context) error {
				return r.ensureResource(ctx, "DynamoDBTable", cfg.LockTableName(),
					r.c.TableExists(ctx, cfg.LockTableName()),
					func(ctx context.Context) error {
						return platformaws.EnsureLockTable(ctx, r.c.DynamoDB, cfg.LockTableName(), r.tags)
					},
				)
			},
		},
		{
			name: "platform-admin-role",
			desc: fmt.Sprintf("ensure IAM role %s (allow *, deny root-account changes, trusted by account root)", config.RoleNamePlatformAdmin),
			run: func(ctx context.Context) error {
				return r.ensureResource(ctx, "IAMRole", config.RoleNamePlatformAdmin,
					r.c.RoleExists(ctx, config.RoleNamePlatformAdmin),
					func(ctx context.Context) error {
						return platformaws.EnsurePlatformAdminRole(ctx, r.c.IAM, config.RoleNamePlatformAdmin, r.c.AccountID, r.tags)
					},
				)
			},
		},
		{
			name: "platform-events-topic",
			desc: fmt.Sprintf("ensure SNS topic %s", cfg.EventsTopicName()),
			run: func(ctx context.Context) error {
				existed := r.c.TopicExists(ctx, cfg.EventsTopicName())
				arn, err := platformaws.EnsureEventsTopic(ctx, r.c.SNS, cfg.EventsTopicName(), r.tags)
				if err != nil {
					return err
				}
				r.topic = arn
				r.log.Info("SNS topic ready", "step", "platform-events-topic", "arn", arn)
				r.postEnsure(ctx, "SNSTopic", cfg.EventsTopicName(), existed)
				return nil
			},
		},
		{
			name: "platform-events-policy",
			desc: fmt.Sprintf("ensure SNS topic %s allows budgets.amazonaws.com to publish", cfg.EventsTopicName()),
			run: func(ctx context.Context) error {
				if r.topic == "" {
					return fmt.Errorf("platform-events-policy requires platform-events-topic to have run first")
				}
				return platformaws.EnsureTopicBudgetPolicy(ctx, r.c.SNS, r.topic, r.c.AccountID)
			},
		},
		{
			name: "platform-budget",
			desc: fmt.Sprintf("ensure monthly cost budget %s (%.2f USD, alerts at 50%%/80%%/100%%)", cfg.BudgetName(), cfg.BudgetMonthlyUSD),
			run: func(ctx context.Context) error {
				if r.topic == "" {
					return fmt.Errorf("platform-budget requires platform-events-topic to have run first")
				}
				return r.ensureResource(ctx, "AWSBudget", cfg.BudgetName(),
					r.c.BudgetExists(ctx, cfg.BudgetName()),
					func(ctx context.Context) error {
						return platformaws.EnsureBudget(ctx, r.c.Budgets, r.c.AccountID, r.topic, cfg.BudgetName(), cfg.BudgetMonthlyUSD)
					},
				)
			},
		},
	}
}

// Run executes the bootstrap sequence in order. Each step is skipped when
// --dry-run is set; the step name and description are logged instead.
// A failure halts the sequence immediately and returns a wrapped error.
//
// Step ordering rationale:
//  1. registry-table  — must exist before RegisterResource is called.
//  2. state-bucket    — S3 for Terraform state.
//  3. lock-table      — DynamoDB for Terraform state locking.
//  4. platform-admin-role — IAM role replacing root credentials.
//  5. platform-events-topic — SNS for observability events.
//  6. platform-events-policy — SNS topic policy for budget notifications.
//  7. platform-budget — monthly cost guardrail.
//
// After each resource step, a platform event is published to the SNS topic.
// SNS failures are non-fatal: the error is logged and the sequence continues.
// The SNS topic ARN is resolved in step "platform-events-topic" and shared
// via closure; steps that run before it skip publishing silently.
//
// After each resource step, RegisterResource is called to record the resource
// in the bootstrap registry table. Registry failures are also non-fatal:
// the resource was already created and the record can be re-written on the
// next run.
func Run(ctx context.Context, cfg *config.Config, clients *platformaws.Clients) error {
	logger := logging.FromContext(ctx)
	logger.Info("bootstrap sequence starting",
		"org", cfg.OrgName,
		"region", cfg.Region,
		"state_region", cfg.StateRegion,
		"dry_run", cfg.DryRun,
		"budget_usd", cfg.BudgetMonthlyUSD,
	)
	if cfg.DryRun {
		// Keep the nil-client dry-run behavior: no dereferences, just step logs.
		_ = runSteps(ctx, true, stepRunStopOnError, "bootstrap", newBootstrapRunner(ctx, cfg, clients).steps())
		logger.Info("bootstrap sequence complete")
		return nil
	}

	r := newBootstrapRunner(ctx, cfg, clients)
	if err := runSteps(ctx, cfg.DryRun, stepRunStopOnError, "bootstrap", r.steps()); err != nil {
		return err
	}

	logger.Info("bootstrap sequence complete")
	return nil
}
