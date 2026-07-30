//go:build integration

package bootstrap

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/ffreis/platform-bootstrap/internal/config"
)

func integrationConfig() *config.Config {
	return &config.Config{
		OrgName:          "acme",
		Region:           "us-east-1",
		StateRegion:      "us-east-1",
		LogLevel:         "info",
		BudgetMonthlyUSD: 25.0,
		Accounts: map[string]string{
			"dev": "dev@example.com",
		},
	}
}

func TestRunHappyPathIntegration(t *testing.T) {
	h := newIntegrationHarness(integrationConfig())
	err := Run(context.Background(), h.cfg, h.clients, io.Discard)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if h.dynamo.createTableCalls != 2 {
		t.Errorf("CreateTable calls: want 2 (registry+lock), got %d", h.dynamo.createTableCalls)
	}
	if h.s3.createCalls != 1 {
		t.Errorf("CreateBucket calls: want 1, got %d", h.s3.createCalls)
	}
	if h.iam.createRoleCalls != 1 {
		t.Errorf("CreateRole calls: want 1, got %d", h.iam.createRoleCalls)
	}
	if h.sns.createTopicCalls != 1 {
		t.Errorf("CreateTopic calls: want 1, got %d", h.sns.createTopicCalls)
	}
	if h.sns.setAttrCalls != 1 {
		t.Errorf("SetTopicAttributes calls: want 1, got %d", h.sns.setAttrCalls)
	}
	if h.budgets.createCalls != 1 {
		t.Errorf("CreateBudget calls: want 1, got %d", h.budgets.createCalls)
	}
	if h.sns.publishCalls != 2 {
		t.Errorf("Publish calls: want 2 events (topic + budget), got %d", h.sns.publishCalls)
	}
	// 7 resource registrations + 1 account-config write.
	//
	// The registering steps are exactly those that reach tryRegister — via
	// ensureResource, or by calling postEnsure directly:
	//   registry-table, register-admin-role, register-admin-user,
	//   state-bucket, lock-table, platform-events-topic, platform-budget
	//
	// platform-admin-role and admin-user do NOT register at creation time (the
	// registry table does not exist yet) — that is what the two register-* back-fill
	// steps exist for. The account-config write is one per cfg.Accounts entry, plus
	// one more only when cfg.AdminEmail is set; integrationConfig() has 1 account and
	// no AdminEmail, so it contributes exactly 1.
	//
	// If this drifts, diff that step list against bootstrapStepDefs rather than just
	// bumping the number — an unexpected +1 can equally mean a resource is being
	// registered TWICE, which is a bug, not a stale count.
	if h.dynamo.putItemCalls != 8 {
		t.Errorf("PutItem calls: want 8 (7 resource registrations + 1 account config), got %d", h.dynamo.putItemCalls)
	}
}

func TestRunIdempotentIntegration(t *testing.T) {
	h := newIntegrationHarness(integrationConfig())
	h.cfg.Accounts = map[string]string{}
	h.s3.bucketExists = true
	h.dynamo.tables = map[string]dbtypes.TableStatus{
		h.cfg.RegistryTableName(): dbtypes.TableStatusActive,
		h.cfg.LockTableName():     dbtypes.TableStatusActive,
	}
	h.iam.roleExists = true
	h.sns.topicExists = true
	h.sns.topicARN = "arn:aws:sns:us-east-1:123456789012:acme-platform-events"
	h.budgets.budgetExists = true

	err := Run(context.Background(), h.cfg, h.clients, io.Discard)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if h.s3.createCalls != 0 {
		t.Errorf("CreateBucket calls: want 0 for existing bucket, got %d", h.s3.createCalls)
	}
	if h.dynamo.createTableCalls != 0 {
		t.Errorf("CreateTable calls: want 0 for existing tables, got %d", h.dynamo.createTableCalls)
	}
	if h.iam.createRoleCalls != 0 {
		t.Errorf("CreateRole calls: want 0 for existing role, got %d", h.iam.createRoleCalls)
	}
	// EnsureEventsTopic uses CreateTopic idempotently on every run.
	if h.sns.createTopicCalls != 1 {
		t.Errorf("CreateTopic calls: want 1, got %d", h.sns.createTopicCalls)
	}
	if h.budgets.createCalls != 0 {
		t.Errorf("CreateBudget calls: want 0 for existing budget, got %d", h.budgets.createCalls)
	}
	if h.sns.publishCalls != 2 {
		t.Errorf("Publish calls: want 2 events (topic + budget), got %d", h.sns.publishCalls)
	}
	// Same 7 registering steps as the happy path — see the comment there for the
	// list and for why platform-admin-role/admin-user are back-filled instead.
	// Registration is unconditional on whether the resource already existed: an
	// idempotent re-run still records current state, so this count does NOT drop
	// when everything pre-exists.
	if h.dynamo.putItemCalls != 7 {
		t.Errorf("PutItem calls: want 7 resource registrations, got %d", h.dynamo.putItemCalls)
	}
}

func TestRunStepFailureIntegration(t *testing.T) {
	h := newIntegrationHarness(integrationConfig())
	h.cfg.Accounts = map[string]string{}
	h.s3.versioningErr = errors.New("versioning failed")

	err := Run(context.Background(), h.cfg, h.clients, io.Discard)
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "step state-bucket") {
		t.Fatalf("error should include failing step name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "enabling versioning") {
		t.Fatalf("error should preserve root cause context, got: %v", err)
	}
}

func TestRunTopicARNGuardIntegration(t *testing.T) {
	h := newIntegrationHarness(integrationConfig())
	h.cfg.Accounts = map[string]string{}
	h.sns.returnNilTopic = true

	err := Run(context.Background(), h.cfg, h.clients, io.Discard)
	if err == nil {
		t.Fatal("Run() expected guard error, got nil")
	}
	if !strings.Contains(err.Error(), "step platform-events-policy") {
		t.Fatalf("error should include failing step name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "requires platform-events-topic") {
		t.Fatalf("error should include topic ordering guard message, got: %v", err)
	}
}
