package bootstrap

import (
	"context"
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

func TestExpectedResources_Count(t *testing.T) {
	resources := ExpectedResources(minimalConfig())
	if len(resources) != 6 {
		t.Errorf("want 6 expected resources, got %d", len(resources))
	}
}

func TestExpectedResources_ContainsAllTypes(t *testing.T) {
	resources := ExpectedResources(minimalConfig())

	typeCounts := map[string]int{}
	for _, r := range resources {
		typeCounts[r.ResourceType]++
	}

	cases := []struct{ typ string; wantMin int }{
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

func TestExpectedResources_RegistryTableFirst(t *testing.T) {
	cfg := minimalConfig()
	resources := ExpectedResources(cfg)

	// The registry table must be first because bootstrap.Run uses it immediately.
	first := resources[0]
	if first.ResourceType != "DynamoDBTable" || first.ResourceName != cfg.RegistryTableName() {
		t.Errorf("first resource: want (DynamoDBTable, %s), got (%s, %s)",
			cfg.RegistryTableName(), first.ResourceType, first.ResourceName)
	}
}

func TestExpectedResources_BudgetLast(t *testing.T) {
	cfg := minimalConfig()
	resources := ExpectedResources(cfg)

	last := resources[len(resources)-1]
	if last.ResourceType != "AWSBudget" || last.ResourceName != cfg.BudgetName() {
		t.Errorf("last resource: want (AWSBudget, %s), got (%s, %s)",
			cfg.BudgetName(), last.ResourceType, last.ResourceName)
	}
}

func TestExpectedResources_NamesMatchConfig(t *testing.T) {
	cfg := minimalConfig()
	resources := ExpectedResources(cfg)

	// Build a set of expected names so we can verify membership.
	wantNames := map[string]bool{
		cfg.RegistryTableName(): true,
		cfg.StateBucketName():   true,
		cfg.LockTableName():     true,
		config.RoleNamePlatformAdmin: true,
		cfg.EventsTopicName():   true,
		cfg.BudgetName():        true,
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
func TestRun_DryRun(t *testing.T) {
	cfg := minimalConfig()
	cfg.DryRun = true

	// All Clients fields are nil — any AWS call would panic immediately,
	// giving us proof that dry-run is fully honoured.
	err := Run(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("Run dry-run: unexpected error: %v", err)
	}
}
