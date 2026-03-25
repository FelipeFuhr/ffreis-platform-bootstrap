package cmd

import (
	"fmt"
	"os"
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

		fmt.Fprintln(os.Stdout, "platform-bootstrap doctor")
		fmt.Fprintf(os.Stdout, "- org: %s\n", deps.cfg.OrgName)
		fmt.Fprintf(os.Stdout, "- region: %s\n", deps.cfg.Region)
		fmt.Fprintf(os.Stdout, "- state_region: %s\n", deps.cfg.StateRegion)
		if deps.cfg.AWSProfile != "" {
			fmt.Fprintf(os.Stdout, "- profile: %s\n", deps.cfg.AWSProfile)
		} else {
			fmt.Fprintln(os.Stdout, "- profile: (none; using environment credentials)")
		}
		fmt.Fprintf(os.Stdout, "- account_id: %s\n", deps.clients.AccountID)
		fmt.Fprintf(os.Stdout, "- caller_arn: %s\n", deps.clients.CallerARN)

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

		fmt.Fprintln(os.Stdout, "\nChecks:")
		failCount := 0
		for _, c := range checks {
			if err := c.run(); err != nil {
				failCount++
				fmt.Fprintf(os.Stdout, "  FAIL   %s\n", c.name)
				fmt.Fprintf(os.Stdout, "         %s\n", c.desc)
				fmt.Fprintf(os.Stdout, "         error: %s\n", formatErr(err))
				continue
			}
			fmt.Fprintf(os.Stdout, "  ok     %s\n", c.name)
		}

		if failCount > 0 {
			fmt.Fprintln(os.Stdout, "\nHints:")
			if deps.cfg.AWSProfile != "" {
				fmt.Fprintf(os.Stdout, "- If this profile uses AWS SSO / IAM Identity Center, run: aws sso login --profile %s\n", deps.cfg.AWSProfile)
			}
			fmt.Fprintln(os.Stdout, "- Ensure you're using an administrator principal in the AWS management account (not a member account).")
			fmt.Fprintln(os.Stdout, "- Required services for init: IAM, S3, DynamoDB, SNS, Budgets.")
			return &ExitError{Code: exitAWSError, Err: fmt.Errorf("doctor failed: %d check(s) failed", failCount)}
		}

		fmt.Fprintln(os.Stdout, "\nAll checks passed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

