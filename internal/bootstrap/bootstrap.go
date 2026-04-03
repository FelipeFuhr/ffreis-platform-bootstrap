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
	var out []ExpectedResource
	for _, def := range bootstrapStepDefs(cfg) {
		if def.resourceType == "" {
			continue
		}
		out = append(out, ExpectedResource{
			ResourceType: string(def.resourceType),
			ResourceName: def.resourceName,
		})
	}
	return out
}

type bootstrapRunner struct {
	cfg           *config.Config
	c             *platformaws.Clients
	log           *slog.Logger
	tags          map[string]string
	registryTable string
	topic         string
	// tempUser is set during the create-temp-user step and consumed by
	// assume-admin-role. It is deleted in the delete-temp-user step.
	tempUser *platformaws.TempUser
	// rootIAM holds the original IAM client (as root) used to delete the
	// temp user, since r.c.IAM is replaced after role assumption.
	rootIAM platformaws.IAMAPI
}

func newBootstrapRunner(ctx context.Context, cfg *config.Config, clients *platformaws.Clients) *bootstrapRunner {
	return &bootstrapRunner{
		cfg:           cfg,
		c:             clients,
		log:           logging.FromContext(ctx),
		tags:          platformaws.RequiredTags(cfg.OrgName, cfg.ToolVersion),
		registryTable: cfg.RegistryTableName(),
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

func (r *bootstrapRunner) tryRegister(ctx context.Context, resourceType ResourceType, resourceName string) {
	rec, err := platformaws.NewRegistryRecord(string(resourceType), resourceName, r.c.CallerARN, r.tags)
	if err != nil {
		r.log.Warn("failed to build registry record, continuing",
			"resource_type", resourceType,
			"resource_name", resourceName,
			"error", err,
		)
		return
	}
	if err := platformaws.RegisterResource(ctx, r.c.DynamoDB, r.registryTable, rec); err != nil {
		r.log.Warn("registry write failed, continuing",
			"resource_type", resourceType,
			"resource_name", resourceName,
			"error", err,
		)
	}
}

func (r *bootstrapRunner) postEnsure(ctx context.Context, resourceType ResourceType, resourceName string, existed *bool) {
	eventType := eventTypeForExistence(existed)
	r.tryPublish(ctx, platformaws.NewEvent(eventType, string(resourceType), resourceName, r.c.CallerARN))
	r.tryRegister(ctx, resourceType, resourceName)
}

func (r *bootstrapRunner) ensureResource(ctx context.Context, resourceType ResourceType, resourceName string, existed *bool, ensureFn func(context.Context) error) error {
	if err := ensureFn(ctx); err != nil {
		return err
	}
	r.postEnsure(ctx, resourceType, resourceName, existed)
	return nil
}

func (r *bootstrapRunner) existedOrUnknown(ctx context.Context, resourceType ResourceType, resourceName string, checkFn func(context.Context, string) (bool, error)) *bool {
	existed, err := checkFn(ctx, resourceName)
	if err != nil {
		r.log.Warn("existence check failed; publishing ensured event",
			"resource_type", resourceType,
			"resource_name", resourceName,
			"error", err,
		)
		return nil
	}
	return &existed
}

func (r *bootstrapRunner) requireTopic(stepName string) error {
	if r.topic == "" {
		return fmt.Errorf("%s requires platform-events-topic to have run first", stepName)
	}
	return nil
}

const stepAccountConfig = "account-config"

// runAccountConfig writes per-account and admin email entries to the registry.
func runAccountConfig(ctx context.Context, r *bootstrapRunner, cfg *config.Config) error {
	for name, email := range cfg.Accounts {
		if err := platformaws.WriteConfig(
			ctx, r.c.DynamoDB, r.registryTable,
			"account", name, r.c.CallerARN,
			map[string]string{"email": email},
		); err != nil {
			return fmt.Errorf("writing account config %q: %w", name, err)
		}
		r.log.Info("account config written", "step", stepAccountConfig, "account", name)
	}
	if cfg.AdminEmail != "" {
		if err := platformaws.WriteConfig(
			ctx, r.c.DynamoDB, r.registryTable,
			"admin", "alert_email", r.c.CallerARN,
			map[string]string{"email": cfg.AdminEmail},
		); err != nil {
			return fmt.Errorf("writing admin alert email config: %w", err)
		}
		r.log.Info("admin config written", "step", stepAccountConfig, "key", "alert_email")
	}
	return nil
}

type bootstrapStepDef struct {
	name         string
	desc         string
	resourceType ResourceType // empty for non-resource steps
	resourceName string
	run          func(context.Context, *bootstrapRunner) error
}

func bootstrapStepDefs(cfg *config.Config) []bootstrapStepDef {
	return []bootstrapStepDef{
		{
			// platform-admin-role has no resourceType/resourceName here because the
			// registry table does not yet exist at this point in the sequence.
			// Registration is handled by the dedicated register-admin-role step
			// that runs after registry-table is created.
			name: "platform-admin-role",
			desc: fmt.Sprintf("ensure IAM role %s (allow *, deny root-account changes, trusted by account root)", config.RoleNamePlatformAdmin),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				name := config.RoleNamePlatformAdmin
				return platformaws.EnsurePlatformAdminRole(ctx, r.c.IAM, name, r.c.AccountID, r.tags)
			},
		},
		{
			// create-temp-user only runs when the caller is the AWS root account.
			// Root cannot call sts:AssumeRole directly; a short-lived IAM user
			// with a narrow assume-role policy is used as a bridge.
			// When not running as root this step is a no-op.
			name: "create-temp-user",
			desc: fmt.Sprintf("create ephemeral IAM user %s to bridge root→platform-admin assumption (root-only)", platformaws.TempBootstrapUserName),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				if !platformaws.IsRootARN(r.c.CallerARN) {
					r.log.Debug("not running as root; skipping temp-user creation")
					return nil
				}
				roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", r.c.AccountID, config.RoleNamePlatformAdmin)
				u, err := platformaws.CreateTempBootstrapUser(ctx, r.c.IAM, roleARN, r.tags)
				if err != nil {
					return fmt.Errorf("creating temp bootstrap user: %w", err)
				}
				r.tempUser = &u
				r.rootIAM = r.c.IAM
				r.log.Info("temp bootstrap user created", "step", "create-temp-user", "user", u.UserName)
				return nil
			},
		},
		{
			name: "assume-admin-role",
			desc: fmt.Sprintf("assume IAM role %s — all subsequent steps run as platform-admin, not root", config.RoleNamePlatformAdmin),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				if r.c.STSRoleAssumer == nil && r.tempUser == nil {
					r.log.Debug("STSRoleAssumer not set and no temp user; skipping role assumption")
					return nil
				}
				roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", r.c.AccountID, config.RoleNamePlatformAdmin)
				var (
					adminClients *platformaws.Clients
					err          error
				)
				if r.tempUser != nil {
					// Running as root: use the temp user's credentials to assume the role.
					adminClients, err = platformaws.AssumeRoleWithTempUser(ctx, r.c, *r.tempUser, roleARN)
					if err != nil {
						return fmt.Errorf("assuming platform-admin role via temp user: %w", err)
					}
				} else {
					adminClients, err = platformaws.AssumeAdminRole(ctx, r.c, roleARN)
					if err != nil {
						return fmt.Errorf("assuming platform-admin role: %w", err)
					}
				}
				r.c = adminClients
				r.log.Info("assumed platform-admin role; subsequent steps run as platform-admin",
					"caller_arn", r.c.CallerARN,
				)
				return nil
			},
		},
		{
			name:         "registry-table",
			desc:         fmt.Sprintf("ensure DynamoDB registry table %s (PAY_PER_REQUEST, PK=PK, SK=SK)", cfg.RegistryTableName()),
			resourceType: ResourceTypeDynamoDBTable,
			resourceName: cfg.RegistryTableName(),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				name := cfg.RegistryTableName()
				existed := r.existedOrUnknown(ctx, ResourceTypeDynamoDBTable, name, r.c.TableExistsChecked)
				return r.ensureResource(ctx, ResourceTypeDynamoDBTable, name, existed, func(ctx context.Context) error {
					return platformaws.EnsureRegistryTable(ctx, r.c.DynamoDB, name, r.tags)
				})
			},
		},
		{
			// register-admin-role back-fills the registry record for platform-admin-role,
			// which was created before the registry table existed.
			name:         "register-admin-role",
			desc:         fmt.Sprintf("register IAM role %s in registry table (back-fill after registry-table creation)", config.RoleNamePlatformAdmin),
			resourceType: ResourceTypeIAMRole,
			resourceName: config.RoleNamePlatformAdmin,
			run: func(ctx context.Context, r *bootstrapRunner) error {
				r.tryRegister(ctx, ResourceTypeIAMRole, config.RoleNamePlatformAdmin)
				return nil
			},
		},
		{
			name: stepAccountConfig,
			desc: fmt.Sprintf("write account and admin configuration to registry table %s", cfg.RegistryTableName()),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				return runAccountConfig(ctx, r, cfg)
			},
		},
		{
			name:         "state-bucket",
			desc:         fmt.Sprintf("ensure S3 state bucket %s (versioning on, public access blocked)", cfg.StateBucketName()),
			resourceType: ResourceTypeS3Bucket,
			resourceName: cfg.StateBucketName(),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				name := cfg.StateBucketName()
				existed := r.existedOrUnknown(ctx, ResourceTypeS3Bucket, name, r.c.BucketExistsChecked)
				return r.ensureResource(ctx, ResourceTypeS3Bucket, name, existed, func(ctx context.Context) error {
					return platformaws.EnsureStateBucket(ctx, r.c.S3, name, cfg.StateRegion, r.tags)
				})
			},
		},
		{
			name:         "lock-table",
			desc:         fmt.Sprintf("ensure DynamoDB lock table %s (PAY_PER_REQUEST, PK=LockID)", cfg.LockTableName()),
			resourceType: ResourceTypeDynamoDBTable,
			resourceName: cfg.LockTableName(),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				name := cfg.LockTableName()
				existed := r.existedOrUnknown(ctx, ResourceTypeDynamoDBTable, name, r.c.TableExistsChecked)
				return r.ensureResource(ctx, ResourceTypeDynamoDBTable, name, existed, func(ctx context.Context) error {
					return platformaws.EnsureLockTable(ctx, r.c.DynamoDB, name, r.tags)
				})
			},
		},
		{
			name:         "platform-events-topic",
			desc:         fmt.Sprintf("ensure SNS topic %s", cfg.EventsTopicName()),
			resourceType: ResourceTypeSNSTopic,
			resourceName: cfg.EventsTopicName(),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				name := cfg.EventsTopicName()
				existed := r.existedOrUnknown(ctx, ResourceTypeSNSTopic, name, r.c.TopicExistsChecked)
				arn, err := platformaws.EnsureEventsTopic(ctx, r.c.SNS, name, r.tags)
				if err != nil {
					return err
				}
				r.topic = arn
				r.log.Info("SNS topic ready", "step", "platform-events-topic", "arn", arn)
				r.postEnsure(ctx, ResourceTypeSNSTopic, name, existed)
				return nil
			},
		},
		{
			name: "platform-events-policy",
			desc: fmt.Sprintf("ensure SNS topic %s allows budgets.amazonaws.com to publish", cfg.EventsTopicName()),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				if err := r.requireTopic("platform-events-policy"); err != nil {
					return err
				}
				return platformaws.EnsureTopicBudgetPolicy(ctx, r.c.SNS, r.topic, r.c.AccountID)
			},
		},
		{
			name:         "platform-budget",
			desc:         fmt.Sprintf("ensure monthly cost budget %s (%.2f USD, alerts at 50%%/80%%/100%%)", cfg.BudgetName(), cfg.BudgetMonthlyUSD),
			resourceType: ResourceTypeAWSBudget,
			resourceName: cfg.BudgetName(),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				if err := r.requireTopic("platform-budget"); err != nil {
					return err
				}
				name := cfg.BudgetName()
				existed := r.existedOrUnknown(ctx, ResourceTypeAWSBudget, name, r.c.BudgetExistsChecked)
				return r.ensureResource(ctx, ResourceTypeAWSBudget, name, existed, func(ctx context.Context) error {
					return platformaws.EnsureBudget(ctx, r.c.Budgets, r.c.AccountID, r.topic, name, cfg.BudgetMonthlyUSD)
				})
			},
		},
		{
			// delete-temp-user is a no-op when tempUser is nil (i.e. we were not
			// running as root, so no temp user was created).
			name: "delete-temp-user",
			desc: fmt.Sprintf("delete ephemeral IAM user %s (root-only cleanup)", platformaws.TempBootstrapUserName),
			run: func(ctx context.Context, r *bootstrapRunner) error {
				if r.tempUser == nil {
					r.log.Debug("no temp user to delete; skipping")
					return nil
				}
				// Use rootIAM: r.c.IAM is now the platform-admin client, but
				// platform-admin also has Allow * so either works. rootIAM is the
				// explicit root client saved before assumption.
				iamClient := r.rootIAM
				if iamClient == nil {
					iamClient = r.c.IAM
				}
				if err := platformaws.DeleteTempBootstrapUser(ctx, iamClient, *r.tempUser); err != nil {
					return fmt.Errorf("deleting temp bootstrap user: %w", err)
				}
				r.log.Info("temp bootstrap user deleted", "step", "delete-temp-user", "user", r.tempUser.UserName)
				r.tempUser = nil
				return nil
			},
		},
	}
}

func (r *bootstrapRunner) steps() []step {
	defs := bootstrapStepDefs(r.cfg)
	steps := make([]step, 0, len(defs))
	for _, def := range defs {
		def := def
		steps = append(steps, step{
			name: def.name,
			desc: def.desc,
			run: func(ctx context.Context) error {
				return def.run(ctx, r)
			},
		})
	}
	return steps
}

// Run executes the bootstrap sequence in order. Each step is skipped when
// --dry-run is set; the step name and description are logged instead.
// A failure halts the sequence immediately and returns a wrapped error.
//
// Step ordering rationale:
//  1. platform-admin-role   — create the IAM role (no registry write; table absent).
//  2. create-temp-user      — root-only: create short-lived IAM user to bridge the
//     root→platform-admin assumption gap.
//  3. assume-admin-role     — assume platform-admin via temp user (root path) or
//     directly (IAM user/role path).
//  4. registry-table        — create the registry table.
//  5. register-admin-role   — back-fill the IAM role record now that the table exists.
//  6. account-config        — write per-account metadata to the registry.
//  7. state-bucket          — S3 for Terraform state.
//  8. lock-table            — DynamoDB for Terraform state locking.
//  9. platform-events-topic — SNS for observability events.
//
// 10. platform-events-policy — SNS topic policy for budget notifications.
// 11. platform-budget        — monthly cost guardrail.
// 12. delete-temp-user       — root-only: delete the ephemeral IAM user.
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
		if err := runSteps(ctx, true, stepRunStopOnError, "bootstrap", newBootstrapRunner(ctx, cfg, clients).steps()); err != nil {
			return err
		}
		logger.Info("bootstrap sequence complete")
		return nil
	}

	if err := validateClientsForBootstrap(clients); err != nil {
		return err
	}

	r := newBootstrapRunner(ctx, cfg, clients)
	if err := runSteps(ctx, cfg.DryRun, stepRunStopOnError, "bootstrap", r.steps()); err != nil {
		return err
	}

	logger.Info("bootstrap sequence complete")
	return nil
}
