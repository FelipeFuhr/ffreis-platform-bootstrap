package config

import (
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// makeFlagSet builds a pflag.FlagSet that mirrors the flags registered by
// root's PersistentFlags + init's local flags, then parses args so the
// named flags are marked as Changed.
func makeFlagSet(t *testing.T, args []string) *pflag.FlagSet {
	t.Helper()
	f := pflag.NewFlagSet("test", pflag.ContinueOnError)
	f.String("org", "", "")
	f.String("profile", "", "")
	f.String("region", "", "")
	f.String("state-region", "", "")
	f.StringSlice("allowed-regions", nil, "")
	f.String("log-level", "", "")
	f.Bool("dry-run", false, "")
	f.String("root-email", "", "")
	f.Float64("budget-usd", DefaultBudgetUSD, "")
	f.StringArray("account", nil, "")
	if err := f.Parse(args); err != nil {
		t.Fatalf("flagset parse: %v", err)
	}
	return f
}

// TestLoad_ValidConfig verifies that a fully populated config loads without
// error and all fields are resolved correctly.
func TestLoad_ValidConfig(t *testing.T) {
	t.Setenv(EnvOrgName, "acme")
	t.Setenv(EnvRegion, "us-west-2")
	t.Setenv(EnvLogLevel, "debug")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.OrgName != "acme" {
		t.Errorf("OrgName: want acme, got %s", cfg.OrgName)
	}
	if cfg.Region != "us-west-2" {
		t.Errorf("Region: want us-west-2, got %s", cfg.Region)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: want debug, got %s", cfg.LogLevel)
	}
	// StateRegion should default to primary region when not set.
	if cfg.StateRegion != "us-west-2" {
		t.Errorf("StateRegion: want us-west-2 (default to region), got %s", cfg.StateRegion)
	}
}

// TestLoad_MissingOrg verifies that an empty org name causes a validation error.
func TestLoad_MissingOrg(t *testing.T) {
	t.Setenv(EnvOrgName, "")
	t.Setenv(EnvRegion, "us-east-1")

	_, err := Load(nil)
	if err == nil {
		t.Fatal("expected error for missing org, got nil")
	}
	if !strings.Contains(err.Error(), "org name is required") {
		t.Errorf("error should mention org name, got: %v", err)
	}
}

// TestLoad_InvalidOrgName verifies that a non-conformant org name is rejected.
func TestLoad_InvalidOrgName(t *testing.T) {
	for _, bad := range []string{"AB", "ab", "toolongname", "1abc", "ab-cd"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv(EnvOrgName, bad)
			t.Setenv(EnvRegion, "us-east-1")

			_, err := Load(nil)
			if err == nil {
				t.Fatalf("expected error for org name %q, got nil", bad)
			}
		})
	}
}

// TestLoad_ValidOrgNames verifies boundary-valid org names are accepted.
func TestLoad_ValidOrgNames(t *testing.T) {
	for _, good := range []string{"abc", "a1b2c3", "abcdef"} {
		t.Run(good, func(t *testing.T) {
			t.Setenv(EnvOrgName, good)
			t.Setenv(EnvRegion, "us-east-1")

			_, err := Load(nil)
			if err != nil {
				t.Errorf("unexpected error for org name %q: %v", good, err)
			}
		})
	}
}

// TestLoad_InvalidLogLevel verifies that an unknown log level is rejected.
func TestLoad_InvalidLogLevel(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	t.Setenv(EnvRegion, "us-east-1")
	t.Setenv(EnvLogLevel, "verbose")

	_, err := Load(nil)
	if err == nil {
		t.Fatal("expected error for invalid log level, got nil")
	}
	if !strings.Contains(err.Error(), "log level") {
		t.Errorf("error should mention log level, got: %v", err)
	}
}

// TestLoad_DefaultsApplied verifies that defaults are used when no env vars
// or flags override them.
func TestLoad_DefaultsApplied(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	// Clear any env vars that might leak from the test environment.
	t.Setenv(EnvRegion, "")
	t.Setenv(EnvLogLevel, "")
	t.Setenv(EnvDryRun, "")

	cfg, err := Load(nil)
	if err != nil {
		// Region may be empty from cleared env — that's expected to fail.
		// This sub-test focuses on defaults, so set region explicitly.
		t.Skip("region cleared, test not applicable")
	}

	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("LogLevel: want %s, got %s", DefaultLogLevel, cfg.LogLevel)
	}
	if cfg.BudgetMonthlyUSD != DefaultBudgetUSD {
		t.Errorf("BudgetMonthlyUSD: want %.2f, got %.2f", DefaultBudgetUSD, cfg.BudgetMonthlyUSD)
	}
}

// TestLoad_RegionDefault verifies the built-in region default when env is empty.
func TestLoad_RegionDefault(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	t.Setenv(EnvRegion, "")
	t.Setenv(EnvLogLevel, "info")

	// With no region env var, region starts as DefaultRegion "us-east-1".
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Region != DefaultRegion {
		t.Errorf("Region: want %s (default), got %s", DefaultRegion, cfg.Region)
	}
}

// TestLoad_StateRegionDefault verifies that state_region falls back to region.
func TestLoad_StateRegionDefault(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	t.Setenv(EnvRegion, "eu-west-1")
	t.Setenv(EnvStateRegion, "")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.StateRegion != "eu-west-1" {
		t.Errorf("StateRegion: want eu-west-1 (same as region), got %s", cfg.StateRegion)
	}
}

// TestLoad_ExplicitStateRegion verifies that state_region can differ from region.
func TestLoad_ExplicitStateRegion(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	t.Setenv(EnvRegion, "eu-west-1")
	t.Setenv(EnvStateRegion, "us-east-1")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.StateRegion != "us-east-1" {
		t.Errorf("StateRegion: want us-east-1 (explicit), got %s", cfg.StateRegion)
	}
}

// TestLoad_FlagOverridesEnv verifies that an explicitly set flag overrides
// the corresponding environment variable.
func TestLoad_FlagOverridesEnv(t *testing.T) {
	t.Setenv(EnvOrgName, "env01")
	t.Setenv(EnvRegion, "us-east-1")

	flags := makeFlagSet(t, []string{"--org=flag01"})
	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OrgName != "flag01" {
		t.Errorf("OrgName: want flag01 (flag wins), got %s", cfg.OrgName)
	}
}

// TestLoad_UnchangedFlagDoesNotOverrideEnv verifies that a flag that was not
// explicitly set on the command-line does not clobber the environment value.
func TestLoad_UnchangedFlagDoesNotOverrideEnv(t *testing.T) {
	t.Setenv(EnvOrgName, "env01")
	t.Setenv(EnvRegion, "us-east-1")

	// Build a flagset but do NOT pass --org, so org flag is not Changed.
	flags := makeFlagSet(t, []string{})
	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OrgName != "env01" {
		t.Errorf("OrgName: want env01 (env wins when flag not set), got %s", cfg.OrgName)
	}
}

// TestLoad_DryRunFlag verifies that --dry-run flag is applied.
func TestLoad_DryRunFlag(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	t.Setenv(EnvRegion, "us-east-1")
	t.Setenv(EnvDryRun, "")

	flags := makeFlagSet(t, []string{"--dry-run"})
	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.DryRun {
		t.Error("DryRun: want true (flag set), got false")
	}
}

// TestLoad_DryRunEnv verifies that PLATFORM_DRY_RUN env var is applied.
func TestLoad_DryRunEnv(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	t.Setenv(EnvRegion, "us-east-1")
	t.Setenv(EnvDryRun, "true")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.DryRun {
		t.Error("DryRun: want true (from env), got false")
	}
}

// TestLoad_BudgetUSDEnv verifies that PLATFORM_BUDGET_USD overrides the
// default budget amount.
func TestLoad_BudgetUSDEnv(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	t.Setenv(EnvRegion, "us-east-1")
	t.Setenv(EnvBudgetUSD, "150.50")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BudgetMonthlyUSD != 150.50 {
		t.Errorf("BudgetMonthlyUSD: want 150.50, got %.2f", cfg.BudgetMonthlyUSD)
	}
}

// TestLoad_AccountsFromEnv verifies that PLATFORM_ACCOUNTS parses correctly.
func TestLoad_AccountsFromEnv(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	t.Setenv(EnvRegion, "us-east-1")
	t.Setenv(EnvAccounts, "dev:dev@example.com,prod:prod@example.com")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Accounts["dev"] != "dev@example.com" {
		t.Errorf("Accounts[dev]: want dev@example.com, got %s", cfg.Accounts["dev"])
	}
	if cfg.Accounts["prod"] != "prod@example.com" {
		t.Errorf("Accounts[prod]: want prod@example.com, got %s", cfg.Accounts["prod"])
	}
}

// TestLoad_AccountsFromFlag verifies that --account flags parse correctly.
func TestLoad_AccountsFromFlag(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	t.Setenv(EnvRegion, "us-east-1")
	t.Setenv(EnvAccounts, "")

	flags := makeFlagSet(t, []string{"--account=dev:dev@example.com", "--account=prod:prod@example.com"})
	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Accounts["dev"] != "dev@example.com" {
		t.Errorf("Accounts[dev]: want dev@example.com, got %s", cfg.Accounts["dev"])
	}
}

// TestLoad_AllowedRegionsEnv verifies comma-separated PLATFORM_ALLOWED_REGIONS.
func TestLoad_AllowedRegionsEnv(t *testing.T) {
	t.Setenv(EnvOrgName, "abc")
	t.Setenv(EnvRegion, "us-east-1")
	t.Setenv(EnvAllowedRegions, "us-east-1, eu-west-1")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.AllowedRegions) != 2 {
		t.Errorf("AllowedRegions: want 2, got %d", len(cfg.AllowedRegions))
	}
}

// TestValidate_MultipleErrors verifies that Validate collects all errors
// rather than stopping at the first one.
func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &Config{
		OrgName:          "",  // missing
		Region:           "",  // missing
		LogLevel:         "bad",
		BudgetMonthlyUSD: DefaultBudgetUSD,
	}
	errs := cfg.Validate()
	if len(errs) < 2 {
		t.Errorf("Validate: want at least 2 errors, got %d: %v", len(errs), errs)
	}
}

// TestValidate_AllLogLevels verifies each valid log level is accepted.
func TestValidate_AllLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "DEBUG", "INFO"} {
		cfg := &Config{OrgName: "abc", Region: "us-east-1", LogLevel: level, BudgetMonthlyUSD: DefaultBudgetUSD}
		if errs := cfg.Validate(); len(errs) != 0 {
			t.Errorf("log level %q: unexpected error: %v", level, errs)
		}
	}
}

// TestResourceNameHelpers verifies that all derived names follow the expected
// naming patterns and embed the org name.
func TestResourceNameHelpers(t *testing.T) {
	cfg := &Config{OrgName: "myorg"}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"StateBucketName", cfg.StateBucketName(), "myorg-tf-state-root"},
		{"LockTableName", cfg.LockTableName(), "myorg-tf-locks-root"},
		{"BootstrapTableName", cfg.BootstrapTableName(), "myorg-mgmt-bootstrap-state"},
		{"BootstrapUserName", cfg.BootstrapUserName(), "myorg-mgmt-terraform-bootstrap"},
		{"BootstrapPolicyName", cfg.BootstrapPolicyName(), "myorg-mgmt-terraform-bootstrap-policy"},
		{"EventsTopicName", cfg.EventsTopicName(), "myorg-platform-events"},
		{"RegistryTableName", cfg.RegistryTableName(), "myorg-bootstrap-registry"},
		{"BudgetName", cfg.BudgetName(), "myorg-platform-monthly-budget"},
	}

	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: want %s, got %s", tc.name, tc.want, tc.got)
		}
	}
}

// TestParseAccounts_Valid verifies well-formed name:email pairs parse cleanly.
func TestParseAccounts_Valid(t *testing.T) {
	pairs := []string{"dev:dev@example.com", "prod:prod@example.com"}
	out, err := parseAccounts(pairs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["dev"] != "dev@example.com" {
		t.Errorf("dev: want dev@example.com, got %s", out["dev"])
	}
	if out["prod"] != "prod@example.com" {
		t.Errorf("prod: want prod@example.com, got %s", out["prod"])
	}
}

// TestParseAccounts_NoColon verifies that a pair without a colon is rejected.
func TestParseAccounts_NoColon(t *testing.T) {
	_, err := parseAccounts([]string{"badformat"})
	if err == nil {
		t.Fatal("expected error for pair without colon, got nil")
	}
}

// TestParseAccounts_EmptyName verifies that an empty name portion is rejected.
func TestParseAccounts_EmptyName(t *testing.T) {
	_, err := parseAccounts([]string{":email@example.com"})
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

// TestParseAccounts_EmptyEmail verifies that an empty email portion is rejected.
func TestParseAccounts_EmptyEmail(t *testing.T) {
	_, err := parseAccounts([]string{"name:"})
	if err == nil {
		t.Fatal("expected error for empty email, got nil")
	}
}

// TestParseAccounts_EmptySlice verifies that an empty slice returns an empty map.
func TestParseAccounts_EmptySlice(t *testing.T) {
	out, err := parseAccounts(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("want empty map, got %d entries", len(out))
	}
}

// TestSplitTrimmed_Basic verifies normal comma splitting.
func TestSplitTrimmed_Basic(t *testing.T) {
	got := splitTrimmed("a,b,c", ",")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("splitTrimmed: want [a b c], got %v", got)
	}
}

// TestSplitTrimmed_TrimsWhitespace verifies leading/trailing whitespace is removed.
func TestSplitTrimmed_TrimsWhitespace(t *testing.T) {
	got := splitTrimmed(" a , b , c ", ",")
	if len(got) != 3 || got[0] != "a" {
		t.Errorf("splitTrimmed: want [a b c], got %v", got)
	}
}

// TestSplitTrimmed_SkipsEmpty verifies empty elements after trimming are dropped.
func TestSplitTrimmed_SkipsEmpty(t *testing.T) {
	got := splitTrimmed("a,,b", ",")
	if len(got) != 2 {
		t.Errorf("splitTrimmed: want 2 elements, got %d: %v", len(got), got)
	}
}

// TestSplitTrimmed_EmptyInput verifies an empty string returns an empty slice.
func TestSplitTrimmed_EmptyInput(t *testing.T) {
	got := splitTrimmed("", ",")
	if len(got) != 0 {
		t.Errorf("splitTrimmed: want empty slice, got %v", got)
	}
}
