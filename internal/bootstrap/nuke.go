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

	steps := []struct {
		name string
		desc string
		run  func() error
	}{
		{
			name: "platform-budget",
			desc: fmt.Sprintf("delete budget %s", cfg.BudgetName()),
			run: func() error {
				return platformaws.DeleteBudget(ctx, clients.Budgets, clients.AccountID, cfg.BudgetName())
			},
		},
		{
			name: "platform-events-topic",
			desc: fmt.Sprintf("delete SNS topic %s", cfg.EventsTopicName()),
			run: func() error {
				return platformaws.DeleteSNSTopic(ctx, clients.SNS, clients.Region, clients.AccountID, cfg.EventsTopicName())
			},
		},
		{
			name: "platform-admin-role",
			desc: fmt.Sprintf("delete IAM role %s", config.RoleNamePlatformAdmin),
			run: func() error {
				return platformaws.DeleteIAMRole(ctx, clients.IAM, config.RoleNamePlatformAdmin)
			},
		},
		{
			name: "lock-table",
			desc: fmt.Sprintf("delete DynamoDB lock table %s", cfg.LockTableName()),
			run: func() error {
				return platformaws.DeleteDynamoDBTable(ctx, clients.DynamoDB, cfg.LockTableName())
			},
		},
		{
			name: "state-bucket",
			desc: fmt.Sprintf("empty and delete S3 state bucket %s", cfg.StateBucketName()),
			run: func() error {
				return platformaws.DeleteStateBucket(ctx, clients.S3, cfg.StateBucketName())
			},
		},
		{
			name: "registry-table",
			desc: fmt.Sprintf("delete DynamoDB registry table %s", cfg.RegistryTableName()),
			run: func() error {
				return platformaws.DeleteDynamoDBTable(ctx, clients.DynamoDB, cfg.RegistryTableName())
			},
		},
	}

	var errs []error
	for _, step := range steps {
		logger.Info("nuke step: "+step.desc, "step", step.name)

		if cfg.DryRun {
			logger.Info("dry-run: skipping", "step", step.name)
			continue
		}

		if err := step.run(); err != nil {
			logger.Error("nuke step failed, continuing",
				"step", step.name,
				"error", err,
			)
			errs = append(errs, fmt.Errorf("step %s: %w", step.name, err))
			continue
		}

		logger.Info("nuke step complete", "step", step.name)
	}

	if len(errs) > 0 {
		return fmt.Errorf("nuke completed with %d error(s): %w", len(errs), errs[0])
	}

	logger.Info("nuke sequence complete")
	return nil
}
