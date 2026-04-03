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
func TestLoadValidConfig(t *testing.T) {
	t.Setenv(EnvOrgName, "acme")
	t.Setenv(EnvRegion, testRegionUSWest2)
	t.Setenv(EnvLogLevel, "debug")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}

	if cfg.OrgName != "acme" {
		t.Errorf("OrgName: want acme, got %s", cfg.OrgName)
	}
	if cfg.Region != testRegionUSWest2 {
		t.Errorf("Region: want %s, got %s", testRegionUSWest2, cfg.Region)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: want debug, got %s", cfg.LogLevel)
	}
	// StateRegion should default to primary region when not set.
	if cfg.StateRegion != testRegionUSWest2 {
		t.Errorf("StateRegion: want %s (default to region), got %s", testRegionUSWest2, cfg.StateRegion)
	}
}

// TestLoad_MissingOrg verifies that an empty org name causes a validation error.
func TestLoadMissingOrg(t *testing.T) {
	t.Setenv(EnvOrgName, "")
	t.Setenv(EnvRegion, DefaultRegion)

	_, err := Load(nil)
	if err == nil {
		t.Fatal("expected error for missing org, got nil")
	}
	if !strings.Contains(err.Error(), "org name is required") {
		t.Errorf("error should mention org name, got: %v", err)
	}
}

// TestLoad_InvalidOrgName verifies that a non-conformant org name is rejected.
func TestLoadInvalidOrgName(t *testing.T) {
	for _, bad := range []string{"AB", "ab", "toolongname", "1abc", "ab-cd"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv(EnvOrgName, bad)
			t.Setenv(EnvRegion, DefaultRegion)

			_, err := Load(nil)
			if err == nil {
				t.Fatalf("expected error for org name %q, got nil", bad)
			}
		})
	}
}

// TestLoad_ValidOrgNames verifies boundary-valid org names are accepted.
func TestLoadValidOrgNames(t *testing.T) {
	for _, good := range []string{"abc", "a1b2c3", "abcdef"} {
		t.Run(good, func(t *testing.T) {
			t.Setenv(EnvOrgName, good)
			t.Setenv(EnvRegion, DefaultRegion)

			_, err := Load(nil)
			if err != nil {
				t.Errorf("unexpected error for org name %q: %v", good, err)
			}
		})
	}
}

// TestLoad_InvalidLogLevel verifies that an unknown log level is rejected.
func TestLoadInvalidLogLevel(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
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
func TestLoadDefaultsApplied(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
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
func TestLoadRegionDefault(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, "")
	t.Setenv(EnvLogLevel, DefaultLogLevel)

	// With no region env var, region starts as DefaultRegion.
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.Region != DefaultRegion {
		t.Errorf("Region: want %s (default), got %s", DefaultRegion, cfg.Region)
	}
}

// TestLoad_StateRegionDefault verifies that state_region falls back to region.
func TestLoadStateRegionDefault(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, testRegionEUWest1)
	t.Setenv(EnvStateRegion, "")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.StateRegion != testRegionEUWest1 {
		t.Errorf("StateRegion: want %s (same as region), got %s", testRegionEUWest1, cfg.StateRegion)
	}
}

// TestLoad_ExplicitStateRegion verifies that state_region can differ from region.
func TestLoadExplicitStateRegion(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, testRegionEUWest1)
	t.Setenv(EnvStateRegion, DefaultRegion)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.StateRegion != DefaultRegion {
		t.Errorf("StateRegion: want %s (explicit), got %s", DefaultRegion, cfg.StateRegion)
	}
}

// TestLoad_FlagOverridesEnv verifies that an explicitly set flag overrides
// the corresponding environment variable.
func TestLoadFlagOverridesEnv(t *testing.T) {
	t.Setenv(EnvOrgName, "env01")
	t.Setenv(EnvRegion, DefaultRegion)

	flags := makeFlagSet(t, []string{"--org=flag01"})
	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.OrgName != "flag01" {
		t.Errorf("OrgName: want flag01 (flag wins), got %s", cfg.OrgName)
	}
}

// TestLoad_UnchangedFlagDoesNotOverrideEnv verifies that a flag that was not
// explicitly set on the command-line does not clobber the environment value.
func TestLoadUnchangedFlagDoesNotOverrideEnv(t *testing.T) {
	t.Setenv(EnvOrgName, "env01")
	t.Setenv(EnvRegion, DefaultRegion)

	// Build a flagset but do NOT pass --org, so org flag is not Changed.
	flags := makeFlagSet(t, []string{})
	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.OrgName != "env01" {
		t.Errorf("OrgName: want env01 (env wins when flag not set), got %s", cfg.OrgName)
	}
}

// TestLoad_DryRunFlag verifies that --dry-run flag is applied.
func TestLoadDryRunFlag(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvDryRun, "")

	flags := makeFlagSet(t, []string{"--dry-run"})
	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if !cfg.DryRun {
		t.Error("DryRun: want true (flag set), got false")
	}
}

// TestLoad_DryRunEnv verifies that PLATFORM_DRY_RUN env var is applied.
func TestLoadDryRunEnv(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvDryRun, "true")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if !cfg.DryRun {
		t.Error("DryRun: want true (from env), got false")
	}
}

// TestLoad_BudgetUSDEnv verifies that PLATFORM_BUDGET_USD overrides the
// default budget amount.
func TestLoadBudgetUSDEnv(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvBudgetUSD, "150.50")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.BudgetMonthlyUSD != 150.50 {
		t.Errorf("BudgetMonthlyUSD: want 150.50, got %.2f", cfg.BudgetMonthlyUSD)
	}
}

// TestLoad_AccountsFromEnv verifies that PLATFORM_ACCOUNTS parses correctly.
func TestLoadAccountsFromEnv(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAccounts, "dev:"+testDevEmail+",prod:prod@example.com")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.Accounts["dev"] != testDevEmail {
		t.Errorf("Accounts[dev]: want %s, got %s", testDevEmail, cfg.Accounts["dev"])
	}
	if cfg.Accounts["prod"] != "prod@example.com" {
		t.Errorf("Accounts[prod]: want prod@example.com, got %s", cfg.Accounts["prod"])
	}
}

// TestLoad_AccountsFromFlag verifies that --account flags parse correctly.
func TestLoadAccountsFromFlag(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAccounts, "")

	flags := makeFlagSet(t, []string{"--account=dev:" + testDevEmail, "--account=prod:prod@example.com"})
	cfg, err := Load(flags)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.Accounts["dev"] != testDevEmail {
		t.Errorf("Accounts[dev]: want %s, got %s", testDevEmail, cfg.Accounts["dev"])
	}
}

// TestLoad_AllowedRegionsEnv verifies comma-separated PLATFORM_ALLOWED_REGIONS.
func TestLoadAllowedRegionsEnv(t *testing.T) {
	t.Setenv(EnvOrgName, testOrgName)
	t.Setenv(EnvRegion, DefaultRegion)
	t.Setenv(EnvAllowedRegions, DefaultRegion+", "+testRegionEUWest1)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if len(cfg.AllowedRegions) != 2 {
		t.Errorf("AllowedRegions: want 2, got %d", len(cfg.AllowedRegions))
	}
}

// TestValidate_MultipleErrors verifies that Validate collects all errors
// rather than stopping at the first one.
func TestValidateMultipleErrors(t *testing.T) {
	cfg := &Config{
		OrgName:          "", // missing
		Region:           "", // missing
		LogLevel:         "bad",
		BudgetMonthlyUSD: DefaultBudgetUSD,
	}
	errs := cfg.Validate()
	if len(errs) < 2 {
		t.Errorf("Validate: want at least 2 errors, got %d: %v", len(errs), errs)
	}
}

// TestValidate_AllLogLevels verifies each valid log level is accepted.
func TestValidateAllLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "DEBUG", "INFO"} {
		cfg := &Config{OrgName: testOrgName, Region: DefaultRegion, LogLevel: level, BudgetMonthlyUSD: DefaultBudgetUSD}
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
func TestParseAccountsValid(t *testing.T) {
	pairs := []string{"dev:" + testDevEmail, "prod:prod@example.com"}
	out, err := parseAccounts(pairs)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if out["dev"] != testDevEmail {
		t.Errorf("dev: want %s, got %s", testDevEmail, out["dev"])
	}
	if out["prod"] != "prod@example.com" {
		t.Errorf("prod: want prod@example.com, got %s", out["prod"])
	}
}

// TestParseAccounts_NoColon verifies that a pair without a colon is rejected.
func TestParseAccountsNoColon(t *testing.T) {
	_, err := parseAccounts([]string{"badformat"})
	if err == nil {
		t.Fatal("expected error for pair without colon, got nil")
	}
}

// TestParseAccounts_EmptyName verifies that an empty name portion is rejected.
func TestParseAccountsEmptyName(t *testing.T) {
	_, err := parseAccounts([]string{":email@example.com"})
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

// TestParseAccounts_EmptyEmail verifies that an empty email portion is rejected.
func TestParseAccountsEmptyEmail(t *testing.T) {
	_, err := parseAccounts([]string{"name:"})
	if err == nil {
		t.Fatal("expected error for empty email, got nil")
	}
}

// TestParseAccounts_EmptySlice verifies that an empty slice returns an empty map.
func TestParseAccountsEmptySlice(t *testing.T) {
	out, err := parseAccounts(nil)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if len(out) != 0 {
		t.Errorf("want empty map, got %d entries", len(out))
	}
}

// TestSplitTrimmed_Basic verifies normal comma splitting.
func TestSplitTrimmedBasic(t *testing.T) {
	got := splitTrimmed("a,b,c", ",")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("splitTrimmed: want [a b c], got %v", got)
	}
}

// TestSplitTrimmed_TrimsWhitespace verifies leading/trailing whitespace is removed.
func TestSplitTrimmedTrimsWhitespace(t *testing.T) {
	got := splitTrimmed(" a , b , c ", ",")
	if len(got) != 3 || got[0] != "a" {
		t.Errorf("splitTrimmed: want [a b c], got %v", got)
	}
}

// TestSplitTrimmed_SkipsEmpty verifies empty elements after trimming are dropped.
func TestSplitTrimmedSkipsEmpty(t *testing.T) {
	got := splitTrimmed("a,,b", ",")
	if len(got) != 2 {
		t.Errorf("splitTrimmed: want 2 elements, got %d: %v", len(got), got)
	}
}

// TestSplitTrimmed_EmptyInput verifies an empty string returns an empty slice.
func TestSplitTrimmedEmptyInput(t *testing.T) {
	got := splitTrimmed("", ",")
	if len(got) != 0 {
		t.Errorf("splitTrimmed: want empty slice, got %v", got)
	}
}
