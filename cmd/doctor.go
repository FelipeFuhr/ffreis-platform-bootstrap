package cmd

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/spf13/cobra"
)

type doctorCheck struct {
	name string
	desc string
	run  func() error
}

func formatErr(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ReplaceAll(err.Error(), "\n", " ")
	return strings.TrimSpace(msg)
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose AWS credentials and required platform-bootstrap permissions",
	Long: `doctor runs a series of read-only AWS API calls to validate that the current
credentials can execute the platform-bootstrap workflow.

This command does not create or modify any AWS resources.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		out := newCommandOutput(cmd, deps.ui)

		if deps.ui != nil {
			out.Header("Platform Bootstrap Doctor", "read-only credential and permission checks")
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

		checks := []doctorCheck{
			{
				name: "iam:get-account-summary",
				desc: "validate IAM read access",
				run: func() error {
					_, err := deps.clients.IAM.GetAccountSummary(ctx, &iam.GetAccountSummaryInput{})
					return err
				},
			},
			{
				name: "s3:list-buckets",
				desc: "validate S3 access (used for tfstate bucket)",
				run: func() error {
					_, err := deps.clients.S3.ListBuckets(ctx, &s3.ListBucketsInput{})
					return err
				},
			},
			{
				name: "dynamodb:list-tables",
				desc: "validate DynamoDB access (used for locks/registry tables)",
				run: func() error {
					_, err := deps.clients.DynamoDB.ListTables(ctx, &dynamodb.ListTablesInput{Limit: aws.Int32(1)})
					return err
				},
			},
			{
				name: "sns:list-topics",
				desc: "validate SNS access (used for platform events)",
				run: func() error {
					_, err := deps.clients.SNS.ListTopics(ctx, &sns.ListTopicsInput{})
					return err
				},
			},
			{
				name: "budgets:describe-budgets",
				desc: "validate Budgets access (used for monthly budget + alerts)",
				run: func() error {
					_, err := deps.clients.Budgets.DescribeBudgets(ctx, &budgets.DescribeBudgetsInput{
						AccountId:  aws.String(deps.clients.AccountID),
						MaxResults: aws.Int32(1),
					})
					return err
				},
			},
		}

		out.Blank()
		out.Line("Checks:")
		failCount := 0
		for _, c := range checks {
			if err := c.run(); err != nil {
				failCount++
				if deps.ui != nil {
					out.Line("  " + deps.ui.Badge("error", "fail") + " " + c.name)
				} else {
					out.Line("  fail " + c.name)
				}
				out.Line("         " + c.desc)
				out.Line("         error: " + formatErr(err))
				continue
			}
			if deps.ui != nil {
				out.Line("  " + deps.ui.Badge("ok", "ok") + " " + c.name)
			} else {
				out.Line("  ok   " + c.name)
			}
		}

		if failCount > 0 {
			out.Blank()
			out.Line("Hints:")
			if deps.cfg.AWSProfile != "" {
				out.Line("- If this profile uses AWS SSO / IAM Identity Center, run: aws sso login --profile " + deps.cfg.AWSProfile)
			}
			out.Line("- Ensure you're using an administrator principal in the AWS management account (not a member account).")
			out.Line("- Required services for init: IAM, S3, DynamoDB, SNS, Budgets.")
			return &ExitError{Code: exitAWSError, Err: fmt.Errorf("doctor failed: %d check(s) failed", failCount)}
		}

		if deps.ui != nil {
			out.Blank()
			out.Status("ok", "ok", "all checks passed")
		} else {
			out.Blank()
			out.Line("All checks passed.")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
