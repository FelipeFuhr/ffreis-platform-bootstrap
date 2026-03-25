package bootstrap

import (
	"context"
	"fmt"

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
	tags := platformaws.RequiredTags(cfg.OrgName)

	logger.Info("bootstrap sequence starting",
		"org", cfg.OrgName,
		"region", cfg.Region,
		"state_region", cfg.StateRegion,
		"dry_run", cfg.DryRun,
		"budget_usd", cfg.BudgetMonthlyUSD,
	)

	// topicARN is populated when the platform-events-topic step runs.
	// Steps that execute before it publish nothing (empty ARN guard in tryPublish).
	var topicARN string

	// registryTable is the name of the registry DynamoDB table. It is set
	// once during the registry-table step so later steps can use it.
	registryTable := cfg.RegistryTableName()

	// tryPublish emits an event to SNS. It never returns an error — a failure
	// is logged and swallowed so that an SNS outage does not abort the bootstrap.
	tryPublish := func(e platformaws.Event) {
		if topicARN == "" {
			return // topic not yet created; skip silently
		}
		if err := platformaws.PublishEvent(ctx, clients.SNS, topicARN, e); err != nil {
			logger.Warn("SNS publish failed, continuing",
				"event_type", e.EventType,
				"resource_type", e.ResourceType,
				"resource_name", e.ResourceName,
				"error", err,
			)
		}
	}

	// tryRegister records the resource in the bootstrap registry. It never
	// returns an error — a failure is logged and swallowed so that a registry
	// write error does not abort a successful resource creation.
	tryRegister := func(resourceType, resourceName string) {
		rec, err := platformaws.NewRegistryRecord(resourceType, resourceName, clients.CallerARN, tags)
		if err != nil {
			logger.Warn("failed to build registry record, continuing",
				"resource_type", resourceType,
				"resource_name", resourceName,
				"error", err,
			)
			return
		}
		if err := platformaws.RegisterResource(ctx, clients.DynamoDB, registryTable, rec); err != nil {
			logger.Warn("registry write failed, continuing",
				"resource_type", resourceType,
				"resource_name", resourceName,
				"error", err,
			)
		}
	}

	// postEnsure publishes the appropriate event and records the resource in the
	// bootstrap registry after a successful ensure operation. It is extracted to
	// eliminate the repeated event-type / publish / register triads in each step.
	postEnsure := func(resourceType, resourceName string, existed bool) {
		eventType := platformaws.EventTypeResourceCreated
		if existed {
			eventType = platformaws.EventTypeResourceExists
		}
		tryPublish(platformaws.NewEvent(eventType, resourceType, resourceName, clients.CallerARN))
		tryRegister(resourceType, resourceName)
	}

	// ensureResource runs ensureFn, then calls postEnsure. It collapses the
	// common ensure → publish → register sequence into one call.
	ensureResource := func(resourceType, resourceName string, existed bool, ensureFn func() error) error {
		if err := ensureFn(); err != nil {
			return err
		}
		postEnsure(resourceType, resourceName, existed)
		return nil
	}

	steps := []struct {
		name string
		desc string
		run  func() error
	}{
		{
			name: "registry-table",
			desc: fmt.Sprintf("ensure DynamoDB registry table %s (PAY_PER_REQUEST, PK=PK, SK=SK)", cfg.RegistryTableName()),
			run: func() error {
				return ensureResource("DynamoDBTable", cfg.RegistryTableName(),
					clients.TableExists(ctx, cfg.RegistryTableName()),
					func() error {
						return platformaws.EnsureRegistryTable(ctx, clients.DynamoDB, cfg.RegistryTableName(), tags)
					})
			},
		},
		{
			name: "account-config",
			desc: fmt.Sprintf("write account and admin configuration to registry table %s", cfg.RegistryTableName()),
			run: func() error {
				for name, email := range cfg.Accounts {
					if err := platformaws.WriteConfig(
						ctx, clients.DynamoDB, registryTable,
						"account", name, clients.CallerARN,
						map[string]string{"email": email},
					); err != nil {
						return fmt.Errorf("writing account config %q: %w", name, err)
					}
					logger.Info("account config written", "step", "account-config", "account", name)
				}
				if cfg.AdminEmail != "" {
					if err := platformaws.WriteConfig(
						ctx, clients.DynamoDB, registryTable,
						"admin", "alert_email", clients.CallerARN,
						map[string]string{"email": cfg.AdminEmail},
					); err != nil {
						return fmt.Errorf("writing admin alert email config: %w", err)
					}
					logger.Info("admin config written", "step", "account-config", "key", "alert_email")
				}
				return nil
			},
		},
		{
			name: "state-bucket",
			desc: fmt.Sprintf("ensure S3 state bucket %s (versioning on, public access blocked)", cfg.StateBucketName()),
			run: func() error {
				return ensureResource("S3Bucket", cfg.StateBucketName(),
					clients.BucketExists(ctx, cfg.StateBucketName()),
					func() error {
						return platformaws.EnsureStateBucket(ctx, clients.S3, cfg.StateBucketName(), cfg.StateRegion, tags)
					})
			},
		},
		{
			name: "lock-table",
			desc: fmt.Sprintf("ensure DynamoDB lock table %s (PAY_PER_REQUEST, PK=LockID)", cfg.LockTableName()),
			run: func() error {
				return ensureResource("DynamoDBTable", cfg.LockTableName(),
					clients.TableExists(ctx, cfg.LockTableName()),
					func() error {
						return platformaws.EnsureLockTable(ctx, clients.DynamoDB, cfg.LockTableName(), tags)
					})
			},
		},
		{
			name: "platform-admin-role",
			desc: fmt.Sprintf("ensure IAM role %s (allow *, deny root-account changes, trusted by account root)", config.RoleNamePlatformAdmin),
			run: func() error {
				return ensureResource("IAMRole", config.RoleNamePlatformAdmin,
					clients.RoleExists(ctx, config.RoleNamePlatformAdmin),
					func() error {
						return platformaws.EnsurePlatformAdminRole(ctx, clients.IAM, config.RoleNamePlatformAdmin, clients.AccountID, tags)
					})
			},
		},
		{
			name: "platform-events-topic",
			desc: fmt.Sprintf("ensure SNS topic %s", cfg.EventsTopicName()),
			run: func() error {
				existed := clients.TopicExists(ctx, cfg.EventsTopicName())
				arn, err := platformaws.EnsureEventsTopic(ctx, clients.SNS, cfg.EventsTopicName(), tags)
				if err != nil {
					return err
				}
				topicARN = arn
				logger.Info("SNS topic ready", "step", "platform-events-topic", "arn", arn)
				postEnsure("SNSTopic", cfg.EventsTopicName(), existed)
				return nil
			},
		},
		{
			name: "platform-events-policy",
			desc: fmt.Sprintf("ensure SNS topic %s allows budgets.amazonaws.com to publish", cfg.EventsTopicName()),
			// requires: platform-events-topic (sets topicARN)
			run: func() error {
				if topicARN == "" {
					return fmt.Errorf("platform-events-policy requires platform-events-topic to have run first")
				}
				return platformaws.EnsureTopicBudgetPolicy(ctx, clients.SNS, topicARN, clients.AccountID)
			},
		},
		{
			name: "platform-budget",
			desc: fmt.Sprintf("ensure monthly cost budget %s (%.2f USD, alerts at 50%%/80%%/100%%)", cfg.BudgetName(), cfg.BudgetMonthlyUSD),
			// requires: platform-events-topic (sets topicARN)
			run: func() error {
				if topicARN == "" {
					return fmt.Errorf("platform-budget requires platform-events-topic to have run first")
				}
				return ensureResource("AWSBudget", cfg.BudgetName(),
					clients.BudgetExists(ctx, cfg.BudgetName()),
					func() error {
						return platformaws.EnsureBudget(ctx, clients.Budgets, clients.AccountID, topicARN, cfg.BudgetName(), cfg.BudgetMonthlyUSD)
					})
			},
		},
	}

	for _, step := range steps {
		logger.Info("step: "+step.desc, "step", step.name)

		if cfg.DryRun {
			logger.Info("dry-run: skipping", "step", step.name)
			continue
		}

		if err := step.run(); err != nil {
			return fmt.Errorf("step %s: %w", step.name, err)
		}

		logger.Info("step complete", "step", step.name)
	}

	logger.Info("bootstrap sequence complete")
	return nil
}
