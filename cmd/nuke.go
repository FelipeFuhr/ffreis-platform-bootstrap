package cmd

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ffreis/platform-bootstrap/internal/bootstrap"
)

var nukeCmd = &cobra.Command{
	Use:   "nuke",
	Short: "Destroy all Layer 0 bootstrap resources",
	Long: `nuke destroys all resources created by init, in reverse order:

  1. AWS Budget
  2. SNS events topic
  3. IAM platform-admin role (inline policies removed first)
  4. Terraform lock DynamoDB table
  5. Terraform state S3 bucket (all versions emptied first)
  6. Bootstrap registry DynamoDB table

WARNING: This is irreversible. All Terraform state and bootstrap configuration
will be permanently lost.

Pass --dry-run to preview what would be deleted without making any AWS calls.`,

	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		out := newCommandOutput(cmd, deps.ui)

		if !deps.cfg.DryRun {
			expected := "nuke-" + deps.cfg.OrgName
			warningTarget := strconv.Quote(deps.cfg.OrgName) + "."
			if deps.ui != nil {
				out.ErrLine(deps.ui.Header("Platform Bootstrap Nuke", "org "+deps.cfg.OrgName))
				out.ErrLine(deps.ui.Badge("error", "warn") + " This will permanently destroy all Layer 0 bootstrap resources for org " + warningTarget)
			} else {
				out.ErrLine("WARNING: This will permanently destroy all Layer 0 bootstrap resources for org " + warningTarget)
			}
			out.ErrLine("Resources to be deleted:")
			out.ErrLine("  - Budget:          " + deps.cfg.BudgetName())
			out.ErrLine("  - SNS topic:       " + deps.cfg.EventsTopicName())
			out.ErrLine("  - IAM role:        platform-admin")
			out.ErrLine("  - DynamoDB table:  " + deps.cfg.LockTableName())
			out.ErrLine("  - S3 bucket:       " + deps.cfg.StateBucketName())
			out.ErrLine("  - DynamoDB table:  " + deps.cfg.RegistryTableName())
			_, _ = io.WriteString(cmd.ErrOrStderr(), "\nType "+strconv.Quote(expected)+" to confirm: ")

			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			reply := strings.TrimSpace(scanner.Text())
			if reply != expected {
				if deps.ui != nil {
					out.ErrStatus("muted", "skip", "operator confirmation did not match")
				} else {
					out.ErrLine("Cancelled.")
				}
				return nil
			}
		}

		if err := bootstrap.Nuke(ctx, deps.cfg, deps.clients); err != nil {
			return &ExitError{Code: exitPartialComplete, Err: err}
		}

		deps.logger.Info("nuke complete",
			"org", deps.cfg.OrgName,
		)
		if deps.ui != nil {
			out.Status("ok", "ok", "bootstrap resources removed")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(nukeCmd)
}
