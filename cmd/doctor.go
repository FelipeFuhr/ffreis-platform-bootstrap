package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose bootstrap permissions, integrity, and exported contract",
	Long: `doctor runs read-only checks against Layer 0 and the contract that
platform-bootstrap exports to downstream stacks.

This command does not create or modify any AWS resources.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		jsonOut, _ := cmd.Flags().GetBool("json")
		out := newCommandOutput(cmd, deps.ui)

		report, err := bootstrapDoctorRunFn(ctx, bootstrapDoctorModes.command)
		if err != nil {
			return &ExitError{Code: exitAWSError, Err: err}
		}

		if jsonOut {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				return &ExitError{Code: exitUserError, Err: err}
			}
		} else {
			if deps.ui != nil {
				out.Header("Platform Bootstrap Doctor", "read-only credential, integrity, and contract checks")
			} else {
				out.Line("platform-bootstrap doctor")
			}
			out.Bullet("org", deps.cfg.OrgName)
			out.Bullet("region", deps.cfg.Region)
			out.Bullet("state_region", deps.cfg.StateRegion)
			if deps.cfg.AWSProfile != "" {
				out.Bullet("profile", deps.cfg.AWSProfile)
			} else {
				out.Bullet("profile", "(none; using environment credentials)")
			}
			out.Bullet("account_id", deps.clients.AccountID)
			out.Bullet("caller_arn", deps.clients.CallerARN)
			out.Blank()
			printBootstrapDoctorReport(out, report)
			out.Blank()
			printBootstrapDoctorSummary(out, report)
		}

		if report.HasFailures() {
			return &ExitError{Code: exitPartialComplete, Err: fmt.Errorf("doctor failed: %d integrity check(s) failed", report.Summary.Fail)}
		}
		return nil
	},
}

func init() {
	doctorCmd.Flags().Bool("json", false, "output the doctor report as JSON")
	rootCmd.AddCommand(doctorCmd)
}
