package config

import "testing"

// TestLoadAdminEmailFromEnv verifies PLATFORM_ADMIN_EMAIL reaches cfg.AdminEmail.
//
// This is the value `bootstrap fetch` emits as budget_alert_emails, and it is the
// one config field deliberately never committed to source control — so the env
// overlay is its only non-flag entry point. An overlay that silently dropped it
// would produce a bootstrap run with no budget alert recipient and no error.
func TestLoadAdminEmailFromEnv(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAdminEmail, "alerts@example.com")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.AdminEmail != "alerts@example.com" {
		t.Errorf("AdminEmail: got %q, want alerts@example.com", cfg.AdminEmail)
	}
}

// TestLoadAdminEmailFromEnvAcceptsCommaSeparatedList verifies the overlay stores
// a multi-recipient value verbatim. Splitting is the consumer's job (fetch emits
// budget_alert_emails); config must not mangle or truncate it here.
func TestLoadAdminEmailFromEnvAcceptsCommaSeparatedList(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAdminEmail, "first@example.com,second@example.com")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.AdminEmail != "first@example.com,second@example.com" {
		t.Errorf("AdminEmail: got %q, want the list stored verbatim", cfg.AdminEmail)
	}
}

// TestLoadAdminEmailUnsetLeavesItEmpty verifies the field stays empty when the
// variable is absent. AdminEmail is optional: an empty value means "write no
// admin config record", which the bootstrap sequence relies on.
func TestLoadAdminEmailUnsetLeavesItEmpty(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAdminEmail, "")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.AdminEmail != "" {
		t.Errorf("AdminEmail: got %q, want empty", cfg.AdminEmail)
	}
}
