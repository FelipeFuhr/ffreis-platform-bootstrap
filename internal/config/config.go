package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

// Config holds all resolved configuration for a single CLI invocation.
// It is populated once in root's PersistentPreRunE and read-only thereafter.
type Config struct {
	OrgName          string
	RootEmail        string
	AWSProfile       string
	Region           string
	StateRegion      string
	AllowedRegions   []string
	LogLevel         string
	DryRun           bool
	BudgetMonthlyUSD float64

	// Accounts maps account name → email for member accounts that should be
	// stored in the bootstrap registry. Populated via --account flags or
	// the PLATFORM_ACCOUNTS environment variable. Optional: an empty map
	// means no account config is written during bootstrap.
	Accounts map[string]string

	// AdminEmail is the address that receives platform budget alert
	// notifications. Stored in the bootstrap registry under CONFIG#admin /
	// alert_email so that platform-bootstrap fetch can emit it into
	// fetched.auto.tfvars.json without ever committing it to source control.
	// Optional: if empty, no admin config record is written and the
	// budget_alert_email Terraform variable must be supplied another way.
	AdminEmail string

	// ToolVersion is the build-time version string injected via linker flags.
	// Set by the cmd layer from the version variable; not user-configurable.
	ToolVersion string
}

// orgNameRe enforces the naming constraint: 3-6 lowercase alphanumeric chars,
// starting with a letter. This prefix appears in every resource name.
var orgNameRe = regexp.MustCompile(`^[a-z][a-z0-9]{2,5}$`)

// Load resolves a Config from three sources in ascending priority order:
//
//  1. Hardcoded defaults (defaults.go)
//  2. PLATFORM_* environment variables (if non-empty)
//  3. CLI flags (only if explicitly set on the command line)
//
// The flags argument is cmd.Flags() from the cobra command being invoked.
// Passing nil is valid and skips flag resolution (useful in tests).
func Load(flags *pflag.FlagSet) (*Config, error) {
	cfg := &Config{
		Region:           DefaultRegion,
		LogLevel:         DefaultLogLevel,
		BudgetMonthlyUSD: DefaultBudgetUSD,
	}

	overlayEnv(cfg)

	if flags != nil {
		overlayFlags(cfg, flags)
	}

	// State region defaults to primary region when not explicitly set.
	if cfg.StateRegion == "" {
		cfg.StateRegion = cfg.Region
	}

	// Admin email defaults to root email when not explicitly set, so that
	// budget alerts are delivered without requiring a separate flag.
	if cfg.AdminEmail == "" {
		cfg.AdminEmail = cfg.RootEmail
	}

	if errs := cfg.Validate(); len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = "  - " + e.Error()
		}
		return nil, fmt.Errorf("invalid configuration:\n%s", strings.Join(msgs, "\n"))
	}

	return cfg, nil
}

// overlayEnv applies non-empty PLATFORM_* environment variables onto cfg.
func overlayEnv(cfg *Config) {
	applyEnvString(EnvOrgName, func(v string) { cfg.OrgName = v })
	applyEnvString(EnvAWSProfile, func(v string) { cfg.AWSProfile = v })
	resolveAWSProfileFallback(cfg)
	applyEnvString(EnvRegion, func(v string) { cfg.Region = v })
	applyEnvString(EnvStateRegion, func(v string) { cfg.StateRegion = v })
	applyEnvString(EnvAllowedRegions, func(v string) { cfg.AllowedRegions = splitTrimmed(v, ",") })
	applyEnvString(EnvLogLevel, func(v string) { cfg.LogLevel = v })
	applyEnvString(EnvDryRun, func(v string) { applyDryRun(cfg, v) })
	applyEnvString(EnvRootEmail, func(v string) { cfg.RootEmail = v })
	applyEnvString(EnvBudgetUSD, func(v string) { applyBudgetUSD(cfg, v) })
	applyEnvString(EnvAccounts, func(v string) { applyAccountsEnv(cfg, v) })
	applyEnvString(EnvAdminEmail, func(v string) { cfg.AdminEmail = v })
}

func applyEnvString(key string, apply func(string)) {
	if v := os.Getenv(key); v != "" {
		apply(v)
	}
}

// resolveAWSProfileFallback falls back to standard AWS env vars when the
// platform-specific profile env is not set. This is especially useful for
// AWS SSO: aws sso login --profile my-sso && AWS_PROFILE=my-sso platform-bootstrap ...
func resolveAWSProfileFallback(cfg *Config) {
	if cfg.AWSProfile != "" {
		return
	}
	if v := os.Getenv(AWSProfileEnv); v != "" {
		cfg.AWSProfile = v
	} else if v := os.Getenv(AWSDefaultProfileEnv); v != "" {
		cfg.AWSProfile = v
	}
}

// applyDryRun parses a boolean string and sets cfg.DryRun on success.
// ParseBool accepts: 1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False.
func applyDryRun(cfg *Config, v string) {
	if b, err := strconv.ParseBool(v); err == nil {
		cfg.DryRun = b
	}
}

// applyBudgetUSD parses a positive float string and sets cfg.BudgetMonthlyUSD on success.
func applyBudgetUSD(cfg *Config, v string) {
	if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
		cfg.BudgetMonthlyUSD = f
	}
}

// applyAccountsEnv parses a comma-separated list of name:email pairs and sets cfg.Accounts on success.
func applyAccountsEnv(cfg *Config, v string) {
	if parsed, err := parseAccounts(splitTrimmed(v, ",")); err == nil {
		cfg.Accounts = parsed
	}
}

// overlayFlags applies CLI flags onto cfg, but only for flags that were
// explicitly set on the command line (pflag.Changed). This ensures that
// a flag's zero-value default does not silently overwrite an env var.
func overlayFlags(cfg *Config, flags *pflag.FlagSet) {
	str := func(key string, dst *string) {
		if flags.Changed(key) {
			*dst, _ = flags.GetString(key)
		}
	}
	f64 := func(key string, dst *float64) {
		if flags.Changed(key) {
			*dst, _ = flags.GetFloat64(key)
		}
	}

	str("org", &cfg.OrgName)
	str("profile", &cfg.AWSProfile)
	str("region", &cfg.Region)
	str("state-region", &cfg.StateRegion)
	if flags.Changed("allowed-regions") {
		cfg.AllowedRegions, _ = flags.GetStringSlice("allowed-regions")
	}
	str("log-level", &cfg.LogLevel)
	if flags.Changed("dry-run") {
		cfg.DryRun, _ = flags.GetBool("dry-run")
	}
	str("root-email", &cfg.RootEmail)
	f64("budget-usd", &cfg.BudgetMonthlyUSD)
	if flags.Changed("account") {
		raw, _ := flags.GetStringArray("account")
		if parsed, err := parseAccounts(raw); err == nil {
			cfg.Accounts = parsed
		}
	}
	str("admin-email", &cfg.AdminEmail)
}

// Validate returns every configuration error found, not just the first.
// Callers should check len(errs) > 0 before proceeding.
func (c *Config) Validate() []error {
	var errs []error

	if c.OrgName == "" {
		errs = append(errs, fmt.Errorf("org name is required (--org or %s)", EnvOrgName))
	} else if !orgNameRe.MatchString(c.OrgName) {
		errs = append(errs, fmt.Errorf(
			"org name %q must be 3-6 lowercase alphanumeric characters starting with a letter",
			c.OrgName,
		))
	}

	if c.Region == "" {
		errs = append(errs, fmt.Errorf("region is required (--region or %s)", EnvRegion))
	}

	valid := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !valid[strings.ToLower(c.LogLevel)] {
		errs = append(errs, fmt.Errorf(
			"log level %q must be one of: debug, info, warn, error",
			c.LogLevel,
		))
	}

	return errs
}

// Resource name helpers — derived deterministically from OrgName.

func (c *Config) StateBucketName() string {
	return fmt.Sprintf(PatternStateBucket, c.OrgName)
}

func (c *Config) LockTableName() string {
	return fmt.Sprintf(PatternLockTable, c.OrgName)
}

func (c *Config) BootstrapTableName() string {
	return fmt.Sprintf(PatternBootstrapTable, c.OrgName)
}

func (c *Config) BootstrapUserName() string {
	return fmt.Sprintf(PatternBootstrapUser, c.OrgName)
}

func (c *Config) BootstrapPolicyName() string {
	return fmt.Sprintf(PatternBootstrapPolicy, c.OrgName)
}

func (c *Config) EventsTopicName() string {
	return fmt.Sprintf(PatternEventsTopic, c.OrgName)
}

func (c *Config) RegistryTableName() string {
	return fmt.Sprintf(PatternRegistryTable, c.OrgName)
}

func (c *Config) BudgetName() string {
	return fmt.Sprintf(PatternBudget, c.OrgName)
}

// parseAccounts converts a slice of "name:email" strings into a map.
// Returns an error if any element is malformed.
func parseAccounts(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		idx := strings.Index(pair, ":")
		if idx <= 0 {
			return nil, fmt.Errorf("account %q must be in name:email format", pair)
		}
		name := strings.TrimSpace(pair[:idx])
		email := strings.TrimSpace(pair[idx+1:])
		if name == "" || email == "" {
			return nil, fmt.Errorf("account %q: name and email must both be non-empty", pair)
		}
		out[name] = email
	}
	return out, nil
}

// splitTrimmed splits s by sep and trims whitespace from each element.
func splitTrimmed(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
