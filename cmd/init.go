package cmd

import (
	"fmt"

	"github.com/ffreis/platform-bootstrap/internal/bootstrap"
	"github.com/ffreis/platform-bootstrap/internal/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Provision Layer 0 — the bootstrap foundation",
	Long: `init provisions the shared AWS resources required before any Terraform can run:

  1. Bootstrap registry DynamoDB table for configuration and metadata
  2. Terraform state S3 bucket
  3. Terraform lock DynamoDB table
  4. Bootstrap IAM role and scoped policy for automation
  5. SNS topic and policy for platform and budget notifications
  6. Monthly AWS Budget and alert subscriptions
  7. Stored bootstrap configuration records (accounts, regions, admin email)

All steps are idempotent. Re-running after a partial failure is safe.
Pass --dry-run to see what would be created without making any changes.`,

	// PreRunE validates init-specific required fields after PersistentPreRunE
	// has already resolved and validated the base Config. The separation keeps
	// each command responsible for its own required inputs.
	PreRunE: func(cmd *cobra.Command, _ []string) error {
		if deps.cfg.RootEmail == "" {
			return &ExitError{
				Code: exitUserError,
				Err: fmt.Errorf(
					"root email is required: set --root-email or %s",
					config.EnvRootEmail,
				),
			}
		}
		return nil
	},

	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		deps.logger.Info("starting platform init",
			"org", deps.cfg.OrgName,
			"region", deps.cfg.Region,
			"state_region", deps.cfg.StateRegion,
			"root_email", deps.cfg.RootEmail,
			"dry_run", deps.cfg.DryRun,
			"account_id", deps.clients.AccountID,
			"caller_arn", deps.clients.CallerARN,
		)

		if err := bootstrap.Run(ctx, deps.cfg, deps.clients); err != nil {
			return &ExitError{Code: exitPartialComplete, Err: err}
		}

		deps.logger.Info("init complete",
			"registry_table", deps.cfg.RegistryTableName(),
			"state_bucket", deps.cfg.StateBucketName(),
			"lock_table", deps.cfg.LockTableName(),
			"events_topic", deps.cfg.EventsTopicName(),
			"budget", deps.cfg.BudgetName(),
		)

		return nil
	},
}

func init() {
	f := initCmd.Flags()

	f.String("root-email", "",
		"root email address of the management account (env: "+config.EnvRootEmail+")")
	f.String("state-region", "",
		"region for the Terraform state bucket and lock table — defaults to --region (env: "+config.EnvStateRegion+")")
	f.StringSlice("allowed-regions", nil,
		"AWS regions to permit via SCP, comma-separated (env: "+config.EnvAllowedRegions+", e.g. us-east-1,eu-west-1)")
	f.Float64("budget-usd", config.DefaultBudgetUSD,
		fmt.Sprintf("monthly cost budget limit in USD (env: %s, default: %.0f)", config.EnvBudgetUSD, config.DefaultBudgetUSD))
	f.StringArray("account", nil,
		"member account in name:email format, repeatable (env: "+config.EnvAccounts+") e.g. --account development:dev@example.com")
	f.String("admin-email", "",
		"email address for platform budget alert notifications — stored in the bootstrap registry, never committed (env: "+config.EnvAdminEmail+")")

	rootCmd.AddCommand(initCmd)
}
