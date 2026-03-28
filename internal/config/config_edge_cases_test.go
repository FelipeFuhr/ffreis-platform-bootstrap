package config

import "testing"

func TestLoad_BudgetUSDEnv_InvalidDoesNotOverrideDefault(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvBudgetUSD, "-1")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.BudgetMonthlyUSD != DefaultBudgetUSD {
		t.Fatalf("BudgetMonthlyUSD: got %.2f, want default %.2f", cfg.BudgetMonthlyUSD, DefaultBudgetUSD)
	}
}

func TestLoad_AccountsEnv_InvalidDoesNotPopulateAccounts(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAccounts, "not-a-pair")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.Accounts != nil {
		t.Fatalf("Accounts: expected nil/empty on invalid env, got %#v", cfg.Accounts)
	}
}
