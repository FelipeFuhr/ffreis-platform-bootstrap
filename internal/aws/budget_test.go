package aws

import (
	"context"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	budgetstypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
)

const (
	testAccountID = "123456789012"
	testBudgetARN = "arn:aws:sns:us-east-1:123456789012:ffreis-platform-events"
	testBudgetName = "ffreis-platform-monthly-budget"
)

// mockBudgets implements BudgetsAPI.
type mockBudgets struct {
	budgetExists  bool
	createCalls   int
	createErr     error
	describeCalls int
}

func (m *mockBudgets) DescribeBudgets(_ context.Context, _ *budgets.DescribeBudgetsInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetsOutput, error) {
	return &budgets.DescribeBudgetsOutput{}, nil
}

func (m *mockBudgets) DescribeBudget(_ context.Context, _ *budgets.DescribeBudgetInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetOutput, error) {
	m.describeCalls++
	if m.budgetExists {
		return &budgets.DescribeBudgetOutput{
			Budget: &budgetstypes.Budget{BudgetName: sdkaws.String(testBudgetName)},
		}, nil
	}
	return nil, &budgetstypes.NotFoundException{}
}

func (m *mockBudgets) CreateBudget(_ context.Context, _ *budgets.CreateBudgetInput, _ ...func(*budgets.Options)) (*budgets.CreateBudgetOutput, error) {
	m.createCalls++
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.budgetExists = true
	return &budgets.CreateBudgetOutput{}, nil
}

func (m *mockBudgets) DeleteBudget(_ context.Context, _ *budgets.DeleteBudgetInput, _ ...func(*budgets.Options)) (*budgets.DeleteBudgetOutput, error) {
	m.budgetExists = false
	return &budgets.DeleteBudgetOutput{}, nil
}

// TestEnsureBudget_Create verifies that when the budget does not exist,
// CreateBudget is called exactly once.
func TestEnsureBudget_Create(t *testing.T) {
	m := &mockBudgets{}

	if err := EnsureBudget(context.Background(), m, testAccountID, testBudgetARN, testBudgetName, 20.0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.createCalls != 1 {
		t.Errorf("createCalls: want 1, got %d", m.createCalls)
	}
}

// TestEnsureBudget_AlreadyExists verifies that when the budget already
// exists, CreateBudget is never called.
func TestEnsureBudget_AlreadyExists(t *testing.T) {
	m := &mockBudgets{budgetExists: true}

	if err := EnsureBudget(context.Background(), m, testAccountID, testBudgetARN, testBudgetName, 20.0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.createCalls != 0 {
		t.Errorf("createCalls: want 0 (budget existed), got %d", m.createCalls)
	}
}

// TestEnsureBudget_Idempotent verifies that calling EnsureBudget twice
// results in exactly one CreateBudget call.
func TestEnsureBudget_Idempotent(t *testing.T) {
	m := &mockBudgets{}

	if err := EnsureBudget(context.Background(), m, testAccountID, testBudgetARN, testBudgetName, 20.0); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if m.createCalls != 1 {
		t.Fatalf("after first call: createCalls want 1, got %d", m.createCalls)
	}

	// Second call — budget now exists (mock state updated by CreateBudget).
	if err := EnsureBudget(context.Background(), m, testAccountID, testBudgetARN, testBudgetName, 20.0); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if m.createCalls != 1 {
		t.Errorf("after second call: createCalls want 1 (no new create), got %d", m.createCalls)
	}
}

// TestEnsureBudget_DuplicateRecordException verifies that a concurrent
// CreateBudget (DuplicateRecordException) is treated as success.
func TestEnsureBudget_DuplicateRecordException(t *testing.T) {
	m := &mockBudgets{
		createErr: &budgetstypes.DuplicateRecordException{},
	}

	if err := EnsureBudget(context.Background(), m, testAccountID, testBudgetARN, testBudgetName, 20.0); err != nil {
		t.Fatalf("expected DuplicateRecordException to be treated as success, got: %v", err)
	}
}

// TestEnsureBudget_ThreeNotifications verifies that three notification
// thresholds (50, 80, 100) are included in the CreateBudget call.
func TestEnsureBudget_ThreeNotifications(t *testing.T) {
	var captured *budgets.CreateBudgetInput

	captureMock := &capturingBudgets{}

	if err := EnsureBudget(context.Background(), captureMock, testAccountID, testBudgetARN, testBudgetName, 20.0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	captured = captureMock.lastInput
	if captured == nil {
		t.Fatal("CreateBudget was not called")
	}

	if len(captured.NotificationsWithSubscribers) != 3 {
		t.Errorf("notifications: want 3, got %d", len(captured.NotificationsWithSubscribers))
	}

	wantThresholds := map[float64]bool{50: true, 80: true, 100: true}
	for _, n := range captured.NotificationsWithSubscribers {
		if !wantThresholds[n.Notification.Threshold] {
			t.Errorf("unexpected threshold: %.0f", n.Notification.Threshold)
		}
	}
}

type capturingBudgets struct {
	lastInput *budgets.CreateBudgetInput
}

func (c *capturingBudgets) DescribeBudgets(_ context.Context, _ *budgets.DescribeBudgetsInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetsOutput, error) {
	return &budgets.DescribeBudgetsOutput{}, nil
}

func (c *capturingBudgets) DescribeBudget(_ context.Context, _ *budgets.DescribeBudgetInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetOutput, error) {
	return nil, &budgetstypes.NotFoundException{}
}

func (c *capturingBudgets) CreateBudget(_ context.Context, params *budgets.CreateBudgetInput, _ ...func(*budgets.Options)) (*budgets.CreateBudgetOutput, error) {
	c.lastInput = params
	return &budgets.CreateBudgetOutput{}, nil
}

func (c *capturingBudgets) DeleteBudget(_ context.Context, _ *budgets.DeleteBudgetInput, _ ...func(*budgets.Options)) (*budgets.DeleteBudgetOutput, error) {
	return &budgets.DeleteBudgetOutput{}, nil
}
