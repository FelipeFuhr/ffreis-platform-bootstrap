package config

// Default values applied before environment variable and flag resolution.
const (
	DefaultRegion   = "us-east-1"
	DefaultLogLevel = "info"
)

// Environment variable names. These are the canonical names for all
// config values that can be supplied without CLI flags (CI, scripting).
const (
	EnvOrgName        = "PLATFORM_ORG"
	EnvAWSProfile     = "PLATFORM_AWS_PROFILE"
	EnvRegion         = "PLATFORM_REGION"
	EnvStateRegion    = "PLATFORM_STATE_REGION"
	EnvAllowedRegions = "PLATFORM_ALLOWED_REGIONS"
	EnvLogLevel       = "PLATFORM_LOG_LEVEL"
	EnvDryRun         = "PLATFORM_DRY_RUN"
	EnvRootEmail      = "PLATFORM_ROOT_EMAIL"
)

// Resource naming patterns. All names are derived from OrgName so that
// no external lookup table is required. Format strings take OrgName as
// the single argument.
const (
	PatternStateBucket      = "%s-tf-state-root"
	PatternLockTable        = "%s-tf-locks-root"
	PatternBootstrapTable   = "%s-mgmt-bootstrap-state"
	PatternBootstrapUser    = "%s-mgmt-terraform-bootstrap"
	PatternBootstrapPolicy  = "%s-mgmt-terraform-bootstrap-policy"
	PatternBootstrapProfile = "bootstrap"

	// RoleNamePlatformAdmin is the IAM role that replaces root for all
	// day-to-day platform administration. Trusted by the account itself.
	RoleNamePlatformAdmin = "platform-admin"

	// PatternEventsTopic is the naming pattern for the platform SNS topic.
	PatternEventsTopic = "%s-platform-events"

	// PatternRegistryTable is the naming pattern for the bootstrap registry
	// DynamoDB table that tracks all platform-managed resources.
	PatternRegistryTable = "%s-bootstrap-registry"

	// PatternBudget is the naming pattern for the monthly AWS Cost Budget.
	PatternBudget = "%s-platform-monthly-budget"

	// DefaultBudgetUSD is the default monthly spend limit in USD.
	DefaultBudgetUSD = 20.0
)

// Environment variable names for budget configuration.
const (
	EnvBudgetUSD = "PLATFORM_BUDGET_USD"
)

// Environment variable name for account configuration.
// Format: comma-separated name:email pairs, e.g.
//
//	PLATFORM_ACCOUNTS=development:dev@example.com,production:prod@example.com
const (
	EnvAccounts = "PLATFORM_ACCOUNTS"
)
