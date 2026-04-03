package bootstrap

import (
	"context"
	"io"
	"testing"

	"github.com/ffreis/platform-bootstrap/internal/config"
)

func TestNukeDryRunAllowsNilClients(t *testing.T) {
	cfg := &config.Config{
		OrgName:          "acme",
		Region:           testRegion,
		StateRegion:      testRegion,
		LogLevel:         "info",
		BudgetMonthlyUSD: 20.0,
		Accounts:         map[string]string{},
		DryRun:           true,
	}

	if err := Nuke(context.Background(), cfg, nil, io.Discard); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNukeNilClientsWhenNotDryRun(t *testing.T) {
	cfg := &config.Config{
		OrgName:          "acme",
		Region:           testRegion,
		StateRegion:      testRegion,
		LogLevel:         "info",
		BudgetMonthlyUSD: 20.0,
		Accounts:         map[string]string{},
		DryRun:           false,
	}

	if err := Nuke(context.Background(), cfg, nil, io.Discard); err == nil {
		t.Fatal("expected error for nil clients when dry-run is false")
	}
}
