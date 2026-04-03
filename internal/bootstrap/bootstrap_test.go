package bootstrap

import (
	"context"
	"io"
	"testing"

	"github.com/ffreis/platform-bootstrap/internal/config"
)

// minimalConfig returns a Config that passes Validate and has a predictable
// org name so resource name helpers are deterministic in tests.
func minimalConfig() *config.Config {
	return &config.Config{
		OrgName:          "acme",
		Region:           "us-east-1",
		StateRegion:      "us-east-1",
		LogLevel:         "info",
		BudgetMonthlyUSD: 20.0,
		Accounts:         map[string]string{},
	}
}

// ── ExpectedResources ─────────────────────────────────────────────────────────

func TestExpectedResourcesCount(t *testing.T) {
	resources := ExpectedResources(minimalConfig())
	if len(resources) != 6 {
		t.Errorf("want 6 expected resources, got %d", len(resources))
	}
}

func TestExpectedResourcesContainsAllTypes(t *testing.T) {
	resources := ExpectedResources(minimalConfig())

	typeCounts := map[string]int{}
	for _, r := range resources {
		typeCounts[r.ResourceType]++
	}

	cases := []struct {
		typ     string
		wantMin int
	}{
		{"DynamoDBTable", 2},
		{"S3Bucket", 1},
		{"IAMRole", 1},
		{"SNSTopic", 1},
		{"AWSBudget", 1},
	}
	for _, tc := range cases {
		if got := typeCounts[tc.typ]; got < tc.wantMin {
			t.Errorf("type %s: want at least %d, got %d", tc.typ, tc.wantMin, got)
		}
	}
}

func TestExpectedResourcesRegistryTableFirst(t *testing.T) {
	cfg := minimalConfig()
	resources := ExpectedResources(cfg)

	// registry-table is the first resource-typed step that appears in
	// ExpectedResources. platform-admin-role is created before it but has no
	// resourceType on its step def (the back-fill happens via register-admin-role
	// which comes after registry-table).
	first := resources[0]
	if first.ResourceType != "DynamoDBTable" || first.ResourceName != cfg.RegistryTableName() {
		t.Errorf("first resource: want (DynamoDBTable, %s), got (%s, %s)",
			cfg.RegistryTableName(), first.ResourceType, first.ResourceName)
	}
}

func TestExpectedResourcesAdminRolePresent(t *testing.T) {
	cfg := minimalConfig()
	resources := ExpectedResources(cfg)

	found := false
	for _, r := range resources {
		if r.ResourceType == "IAMRole" && r.ResourceName == "platform-admin" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected IAMRole/platform-admin in ExpectedResources")
	}
}

func TestExpectedResourcesBudgetLast(t *testing.T) {
	cfg := minimalConfig()
	resources := ExpectedResources(cfg)

	last := resources[len(resources)-1]
	if last.ResourceType != "AWSBudget" || last.ResourceName != cfg.BudgetName() {
		t.Errorf("last resource: want (AWSBudget, %s), got (%s, %s)",
			cfg.BudgetName(), last.ResourceType, last.ResourceName)
	}
}

func TestExpectedResourcesNamesMatchConfig(t *testing.T) {
	cfg := minimalConfig()
	resources := ExpectedResources(cfg)

	// Build a set of expected names so we can verify membership.
	wantNames := map[string]bool{
		cfg.RegistryTableName():      true,
		cfg.StateBucketName():        true,
		cfg.LockTableName():          true,
		config.RoleNamePlatformAdmin: true,
		cfg.EventsTopicName():        true,
		cfg.BudgetName():             true,
	}
	for _, r := range resources {
		if !wantNames[r.ResourceName] {
			t.Errorf("unexpected resource name %q (type %s)", r.ResourceName, r.ResourceType)
		}
	}
}

// ── Run (dry-run) ─────────────────────────────────────────────────────────────

// TestRun_DryRun verifies that when DryRun is true, Run returns nil and makes
// no AWS calls (the nil Clients field pointers are never dereferenced).
func TestRunDryRun(t *testing.T) {
	cfg := minimalConfig()
	cfg.DryRun = true

	// All Clients fields are nil — any AWS call would panic immediately,
	// giving us proof that dry-run is fully honoured.
	err := Run(context.Background(), cfg, nil, io.Discard)
	if err != nil {
		t.Fatalf("Run dry-run: unexpected error: %v", err)
	}
}

func TestRunNilClientsWhenNotDryRun(t *testing.T) {
	cfg := minimalConfig()
	cfg.DryRun = false

	err := Run(context.Background(), cfg, nil, io.Discard)
	if err == nil {
		t.Fatal("expected error for nil clients when dry-run is false")
	}
}
