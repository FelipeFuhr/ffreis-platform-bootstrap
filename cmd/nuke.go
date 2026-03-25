package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ffreis/platform-bootstrap/internal/bootstrap"
	"github.com/spf13/cobra"
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

		if !deps.cfg.DryRun {
			expected := "nuke-" + deps.cfg.OrgName
			fmt.Fprintf(os.Stderr, "WARNING: This will permanently destroy all Layer 0 bootstrap resources for org %q.\n", deps.cfg.OrgName)
			fmt.Fprintf(os.Stderr, "Resources to be deleted:\n")
			fmt.Fprintf(os.Stderr, "  - Budget:          %s\n", deps.cfg.BudgetName())
			fmt.Fprintf(os.Stderr, "  - SNS topic:       %s\n", deps.cfg.EventsTopicName())
			fmt.Fprintf(os.Stderr, "  - IAM role:        platform-admin\n")
			fmt.Fprintf(os.Stderr, "  - DynamoDB table:  %s\n", deps.cfg.LockTableName())
			fmt.Fprintf(os.Stderr, "  - S3 bucket:       %s\n", deps.cfg.StateBucketName())
			fmt.Fprintf(os.Stderr, "  - DynamoDB table:  %s\n", deps.cfg.RegistryTableName())
			fmt.Fprintf(os.Stderr, "\nType %q to confirm: ", expected)

			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			reply := strings.TrimSpace(scanner.Text())
			if reply != expected {
				fmt.Fprintln(os.Stderr, "Cancelled.")
				return nil
			}
		}

		if err := bootstrap.Nuke(ctx, deps.cfg, deps.clients); err != nil {
			return &ExitError{Code: exitPartialComplete, Err: err}
		}

		deps.logger.Info("nuke complete",
			"org", deps.cfg.OrgName,
		)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(nukeCmd)
}
