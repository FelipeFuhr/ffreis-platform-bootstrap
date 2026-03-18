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
	if v := os.Getenv(EnvOrgName); v != "" {
		cfg.OrgName = v
	}
	if v := os.Getenv(EnvAWSProfile); v != "" {
		cfg.AWSProfile = v
	}
	if v := os.Getenv(EnvRegion); v != "" {
		cfg.Region = v
	}
	if v := os.Getenv(EnvStateRegion); v != "" {
		cfg.StateRegion = v
	}
	if v := os.Getenv(EnvAllowedRegions); v != "" {
		cfg.AllowedRegions = splitTrimmed(v, ",")
	}
	if v := os.Getenv(EnvLogLevel); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv(EnvDryRun); v != "" {
		// ParseBool accepts: 1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False.
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.DryRun = b
		}
	}
	if v := os.Getenv(EnvRootEmail); v != "" {
		cfg.RootEmail = v
	}
	if v := os.Getenv(EnvBudgetUSD); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.BudgetMonthlyUSD = f
		}
	}
	if v := os.Getenv(EnvAccounts); v != "" {
		if parsed, err := parseAccounts(splitTrimmed(v, ",")); err == nil {
			cfg.Accounts = parsed
		}
	}
}

// overlayFlags applies CLI flags onto cfg, but only for flags that were
// explicitly set on the command line (pflag.Changed). This ensures that
// a flag's zero-value default does not silently overwrite an env var.
func overlayFlags(cfg *Config, flags *pflag.FlagSet) {
	if flags.Changed("org") {
		cfg.OrgName, _ = flags.GetString("org")
	}
	if flags.Changed("profile") {
		cfg.AWSProfile, _ = flags.GetString("profile")
	}
	if flags.Changed("region") {
		cfg.Region, _ = flags.GetString("region")
	}
	if flags.Changed("state-region") {
		cfg.StateRegion, _ = flags.GetString("state-region")
	}
	if flags.Changed("allowed-regions") {
		cfg.AllowedRegions, _ = flags.GetStringSlice("allowed-regions")
	}
	if flags.Changed("log-level") {
		cfg.LogLevel, _ = flags.GetString("log-level")
	}
	if flags.Changed("dry-run") {
		cfg.DryRun, _ = flags.GetBool("dry-run")
	}
	if flags.Changed("root-email") {
		cfg.RootEmail, _ = flags.GetString("root-email")
	}
	if flags.Changed("budget-usd") {
		cfg.BudgetMonthlyUSD, _ = flags.GetFloat64("budget-usd")
	}
	if flags.Changed("account") {
		raw, _ := flags.GetStringArray("account")
		if parsed, err := parseAccounts(raw); err == nil {
			cfg.Accounts = parsed
		}
	}
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
