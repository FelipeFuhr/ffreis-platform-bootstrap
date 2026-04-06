package cmd

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/ffreis/platform-bootstrap/internal/bootstrap"
	"github.com/ffreis/platform-bootstrap/internal/config"
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
		orgDir, _ := cmd.Flags().GetString("org-dir")
		out := newCommandOutput(cmd, deps.ui)
		if deps.ui != nil {
			out.Header("Platform Bootstrap Init", orgRegionSummary(deps.cfg.OrgName, deps.cfg.Region))
			out.Summary("Config",
				"state="+deps.cfg.StateRegion,
				"dry-run="+strconv.FormatBool(deps.cfg.DryRun),
				"account="+deps.clients.AccountID,
			)
		}

		deps.logger.Info("starting platform init",
			"org", deps.cfg.OrgName,
			"region", deps.cfg.Region,
			"state_region", deps.cfg.StateRegion,
			"root_email", deps.cfg.RootEmail,
			"dry_run", deps.cfg.DryRun,
			"account_id", deps.clients.AccountID,
			"caller_arn", deps.clients.CallerARN,
		)

		doctorReport, err := bootstrapDoctorRunFn(ctx, bootstrapDoctorModes.init)
		if err != nil {
			return &ExitError{Code: exitAWSError, Err: fmt.Errorf("bootstrap doctor preflight: %w", err)}
		}
		if deps.ui != nil {
			out.Status("info", "doctor", "running bootstrap preflight checks")
			printBootstrapDoctorSummary(out, doctorReport)
		}
		if doctorReport.HasFailures() {
			if deps.ui != nil {
				out.Blank()
				printBootstrapDoctorReport(out, doctorReport)
			}
			return &ExitError{Code: exitPartialComplete, Err: fmt.Errorf("bootstrap doctor preflight failed with %d blocking check(s)", doctorReport.Summary.Fail)}
		}

		if err := initBootstrapRunFn(ctx, deps.cfg, deps.clients, cmd.ErrOrStderr()); err != nil {
			return &ExitError{Code: exitPartialComplete, Err: err}
		}

		deps.logger.Info("init complete",
			"registry_table", deps.cfg.RegistryTableName(),
			"state_bucket", deps.cfg.StateBucketName(),
			"lock_table", deps.cfg.LockTableName(),
			"events_topic", deps.cfg.EventsTopicName(),
			"budget", deps.cfg.BudgetName(),
		)
		if deps.ui != nil {
			out.Summary("Outputs",
				deps.cfg.RegistryTableName(),
				deps.cfg.StateBucketName(),
				deps.cfg.LockTableName(),
				deps.cfg.EventsTopicName(),
				deps.cfg.BudgetName(),
			)
		}

		// If --org-dir is provided, generate the config files that platform-org
		// needs to run terraform init + apply, so the operator can proceed
		// immediately without a separate fetch step.
		if orgDir != "" && !deps.cfg.DryRun {
			tfvarsPath := filepath.Join(orgDir, "terraform", "envs", "prod", "fetched.auto.tfvars.json")
			backendPath := filepath.Join(orgDir, "terraform", "stack", "backend.local.hcl")
			nextStep := "cd " + filepath.Join(orgDir, "terraform", "stack") + " && terraform init -backend-config=backend.local.hcl -backend-config=../envs/prod/backend.hcl && terraform apply -var-file=../envs/prod/terraform.tfvars"

			deps.logger.Info("writing org layer config files",
				"tfvars", tfvarsPath,
				"backend", backendPath,
			)

			if err := initWriteFetchedConfigFn(tfvarsPath, backendPath); err != nil {
				deps.logger.Warn("failed to write org config files — run 'platform-bootstrap fetch' manually",
					"error", err,
				)
				if deps.ui != nil {
					out.Status("warn", "warn", "org layer config generation failed: "+err.Error())
				}
			} else {
				deps.logger.Info("org layer ready",
					"next_step", nextStep,
				)
				if deps.ui != nil {
					out.Status("info", "next", nextStep)
				}
			}
		}

		return nil
	},
}

var (
	initBootstrapRunFn       = bootstrap.Run
	initWriteFetchedConfigFn = writeFetchedConfig
)

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
	f.String("org-dir", "",
		"path to the sibling platform org Terraform repo; when set, org config files are written automatically after init completes (e.g. ../your-platform-org-repo)")

	rootCmd.AddCommand(initCmd)
}
