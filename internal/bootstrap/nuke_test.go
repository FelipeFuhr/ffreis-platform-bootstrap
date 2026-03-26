package bootstrap

import (
	"context"
	"testing"

	"github.com/ffreis/platform-bootstrap/internal/config"
)

func TestNuke_DryRunAllowsNilClients(t *testing.T) {
	cfg := &config.Config{
		OrgName:          "acme",
		Region:           "us-east-1",
		StateRegion:      "us-east-1",
		LogLevel:         "info",
		BudgetMonthlyUSD: 20.0,
		Accounts:         map[string]string{},
		DryRun:           true,
	}

	if err := Nuke(context.Background(), cfg, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNuke_NilClientsWhenNotDryRun(t *testing.T) {
	cfg := &config.Config{
		OrgName:          "acme",
		Region:           "us-east-1",
		StateRegion:      "us-east-1",
		LogLevel:         "info",
		BudgetMonthlyUSD: 20.0,
		Accounts:         map[string]string{},
		DryRun:           false,
	}

	if err := Nuke(context.Background(), cfg, nil); err == nil {
		t.Fatal("expected error for nil clients when dry-run is false")
	}
}
