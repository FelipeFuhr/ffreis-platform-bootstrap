package bootstrap

import (
	"context"
	"fmt"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
	"github.com/ffreis/platform-bootstrap/internal/logging"
)

// Nuke destroys all resources created by Run, in reverse order.
//
// Steps (reverse of bootstrap sequence):
//  1. platform-budget       — delete the monthly cost budget
//  2. platform-events-topic — delete the SNS events topic
//  3. platform-admin-role   — delete all inline policies then the IAM role
//  4. lock-table            — delete the Terraform state lock DynamoDB table
//  5. state-bucket          — empty all versions then delete the S3 bucket
//  6. registry-table        — delete the bootstrap registry DynamoDB table
//
// Each step is attempted even if a previous step fails, so that a partial
// state can be cleaned up by re-running nuke. All errors are collected and
// returned together at the end.
//
// Pass cfg.DryRun = true to log what would be deleted without making any
// AWS API calls.
func Nuke(ctx context.Context, cfg *config.Config, clients *platformaws.Clients) error {
	logger := logging.FromContext(ctx)

	logger.Info("nuke sequence starting",
		"org", cfg.OrgName,
		"region", cfg.Region,
		"dry_run", cfg.DryRun,
	)

	steps := []step{
		{
			name: "platform-budget",
			desc: fmt.Sprintf("delete budget %s", cfg.BudgetName()),
			run: func(ctx context.Context) error {
				return platformaws.DeleteBudget(ctx, clients.Budgets, clients.AccountID, cfg.BudgetName())
			},
		},
		{
			name: "platform-events-topic",
			desc: fmt.Sprintf("delete SNS topic %s", cfg.EventsTopicName()),
			run: func(ctx context.Context) error {
				return platformaws.DeleteSNSTopic(ctx, clients.SNS, clients.Region, clients.AccountID, cfg.EventsTopicName())
			},
		},
		{
			name: "platform-admin-role",
			desc: fmt.Sprintf("delete IAM role %s", config.RoleNamePlatformAdmin),
			run: func(ctx context.Context) error {
				return platformaws.DeleteIAMRole(ctx, clients.IAM, config.RoleNamePlatformAdmin)
			},
		},
		{
			name: "lock-table",
			desc: fmt.Sprintf("delete DynamoDB lock table %s", cfg.LockTableName()),
			run: func(ctx context.Context) error {
				return platformaws.DeleteDynamoDBTable(ctx, clients.DynamoDB, cfg.LockTableName())
			},
		},
		{
			name: "state-bucket",
			desc: fmt.Sprintf("empty and delete S3 state bucket %s", cfg.StateBucketName()),
			run: func(ctx context.Context) error {
				return platformaws.DeleteStateBucket(ctx, clients.S3, cfg.StateBucketName())
			},
		},
		{
			name: "registry-table",
			desc: fmt.Sprintf("delete DynamoDB registry table %s", cfg.RegistryTableName()),
			run: func(ctx context.Context) error {
				return platformaws.DeleteDynamoDBTable(ctx, clients.DynamoDB, cfg.RegistryTableName())
			},
		},
	}

	if err := runSteps(ctx, cfg.DryRun, stepRunContinueOnError, "nuke", steps); err != nil {
		return err
	}

	logger.Info("nuke sequence complete")
	return nil
}
