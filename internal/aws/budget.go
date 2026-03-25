package aws

import (
	"context"
	"errors"
	"fmt"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	budgetstypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
)

// EnsureBudget creates a monthly cost budget with SNS alert notifications
// at 50%, 80%, and 100% of the monthly limit.
//
// Idempotency:
//   - DescribeBudget succeeds → budget exists; return nil immediately.
//   - CreateBudget returns DuplicateRecordException → concurrent creation;
//     treat as success.
//
// The SNS topic must already have a policy allowing budgets.amazonaws.com
// to publish (see EnsureTopicBudgetPolicy in sns.go).
func EnsureBudget(ctx context.Context, client BudgetsAPI, accountID, topicARN, budgetName string, monthlyUSD float64) error {
	// Check whether the budget already exists.
	_, err := client.DescribeBudget(ctx, &budgets.DescribeBudgetInput{
		AccountId:  sdkaws.String(accountID),
		BudgetName: sdkaws.String(budgetName),
	})
	if err == nil {
		// Budget already exists — nothing to do.
		return nil
	}

	var notFound *budgetstypes.NotFoundException
	if !errors.As(err, &notFound) {
		return fmt.Errorf("checking budget %s: %w", budgetName, err)
	}

	// Budget does not exist — create it with three threshold notifications.
	thresholds := []float64{50, 80, 100}
	notifications := make([]budgetstypes.NotificationWithSubscribers, 0, len(thresholds))
	for _, pct := range thresholds {
		notifications = append(notifications, budgetstypes.NotificationWithSubscribers{
			Notification: &budgetstypes.Notification{
				ComparisonOperator: budgetstypes.ComparisonOperatorGreaterThan,
				NotificationType:   budgetstypes.NotificationTypeActual,
				Threshold:          pct,
				ThresholdType:      budgetstypes.ThresholdTypePercentage,
			},
			Subscribers: []budgetstypes.Subscriber{
				{
					Address:          sdkaws.String(topicARN),
					SubscriptionType: budgetstypes.SubscriptionTypeSns,
				},
			},
		})
	}

	_, createErr := client.CreateBudget(ctx, &budgets.CreateBudgetInput{
		AccountId: sdkaws.String(accountID),
		Budget: &budgetstypes.Budget{
			BudgetName: sdkaws.String(budgetName),
			BudgetType: budgetstypes.BudgetTypeCost,
			TimeUnit:   budgetstypes.TimeUnitMonthly,
			BudgetLimit: &budgetstypes.Spend{
				Amount: sdkaws.String(fmt.Sprintf("%.2f", monthlyUSD)),
				Unit:   sdkaws.String("USD"),
			},
		},
		NotificationsWithSubscribers: notifications,
	})
	if createErr != nil {
		// DuplicateRecordException means a concurrent run created the budget.
		var dup *budgetstypes.DuplicateRecordException
		if errors.As(createErr, &dup) {
			return nil
		}
		return fmt.Errorf("creating budget %s: %w", budgetName, createErr)
	}
	return nil
}
