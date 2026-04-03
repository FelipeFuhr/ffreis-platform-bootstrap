package cmd

import (
	"errors"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
	"github.com/ffreis/platform-bootstrap/internal/logging"
	platformui "github.com/ffreis/platform-bootstrap/internal/ui"
)

// deps is populated by root's PersistentPreRunE and is available to all
// subcommands. It is intentionally package-scoped rather than global to
// keep it accessible within cmd/ without being reachable from other packages.
var deps struct {
	cfg     *config.Config
	logger  *slog.Logger
	clients *platformaws.Clients
	ui      *platformui.Presenter
}

// ExitError carries a specific exit code alongside an error message.
// Subcommands return this type when they need to signal a non-generic exit.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

const (
	exitOK              = 0
	exitUserError       = 1
	exitAWSError        = 2
	exitPartialComplete = 3
)

var rootCmd = &cobra.Command{
	Use:   "platform-bootstrap",
	Short: "Bootstrap and manage the AWS multi-account platform",
	Long: `platform-bootstrap provisions the foundational AWS infrastructure
for a multi-account platform starting from root credentials.

It is designed to be run in strict layer order. All operations are
idempotent and safe to re-run after a partial failure.

Environment variables mirror every flag (see --help for each command).
Flags take precedence over environment variables.`,

	// Silence cobra's own error and usage printing so we control the format.
	SilenceErrors: true,
	SilenceUsage:  true,

	// PersistentPreRunE runs before every subcommand's PreRunE/RunE.
	// It resolves the full config (flags > env > defaults), initialises the
	// structured logger, and stores both in deps for subcommands to read.
	//
	// cmd.Flags() at this point is the leaf command's complete FlagSet,
	// which includes both inherited persistent flags and the leaf's own
	// local flags. This means root-email (defined on the init subcommand)
	// is available here when `platform-bootstrap init` is invoked.
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := config.Load(cmd.Flags())
		if err != nil {
			return &ExitError{Code: exitUserError, Err: err}
		}
		cfg.ToolVersion = version

		requestedUI, _ := cmd.Flags().GetString("ui")
		presenter, err := platformui.New(requestedUI)
		if err != nil {
			return &ExitError{Code: exitUserError, Err: err}
		}

		logger := logging.New(cfg.LogLevel, !logging.IsTTY())
		logger.Debug("configuration resolved",
			"org", cfg.OrgName,
			"region", cfg.Region,
			"state_region", cfg.StateRegion,
			"profile", cfg.AWSProfile,
			"dry_run", cfg.DryRun,
		)

		deps.cfg = cfg
		deps.logger = logger
		deps.ui = presenter

		// Propagate the logger through context so subcommands and internal
		// packages can retrieve it via logging.FromContext(ctx) without
		// importing deps directly.
		ctx := logging.WithLogger(cmd.Context(), logger)
		ctx = platformui.WithPresenter(ctx, presenter)
		cmd.SetContext(ctx)

		// Validate credentials immediately. Any bootstrap step that calls AWS
		// requires a verified identity, and failing here — before any writes —
		// produces the clearest possible error message.
		//
		// Error classification:
		//   ErrNoCredentials → exitUserError  (operator forgot to set credentials)
		//   all other errors → exitAWSError   (invalid key, auth failure, network)
		clients, err := platformaws.New(ctx, cfg)
		if err != nil {
			code := exitAWSError
			if errors.Is(err, platformaws.ErrNoCredentials) {
				code = exitUserError
			}
			return &ExitError{Code: code, Err: err}
		}

		logger.Info("credentials verified",
			"account_id", clients.AccountID,
			"caller_arn", clients.CallerARN,
			"region", clients.Region,
		)

		deps.clients = clients

		return nil
	},
}

// Execute is the single entry point called by main.
// It maps command errors to exit codes and writes human-readable errors to stderr.
func Execute() int {
	return executeCommand(rootCmd, os.Stderr)
}

func executeCommand(cmd *cobra.Command, stderr io.Writer) int {
	if err := cmd.Execute(); err != nil {
		if message := err.Error(); message != "" {
			_, _ = io.WriteString(stderr, "error: "+message+"\n")
		}
		return exitCodeForError(err)
	}
	return exitOK
}

func exitCodeForError(err error) int {
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return exitUserError
}

func init() {
	// Persistent flags are inherited by every subcommand.
	f := rootCmd.PersistentFlags()

	f.String("org", "",
		"short org identifier, 3-6 lowercase alphanumeric chars (env: "+config.EnvOrgName+")")
	f.String("profile", "",
		"AWS named profile for credentials (env: "+config.EnvAWSProfile+")")
	f.String("region", "",
		"primary AWS region (env: "+config.EnvRegion+", default: "+config.DefaultRegion+")")
	f.String("log-level", "",
		"log verbosity: debug, info, warn, error (env: "+config.EnvLogLevel+", default: "+config.DefaultLogLevel+")")
	f.Bool("dry-run", false,
		"describe actions without executing any AWS calls (env: "+config.EnvDryRun+")")
	f.String("ui", "auto",
		"UI mode: auto, plain, rich")
}
