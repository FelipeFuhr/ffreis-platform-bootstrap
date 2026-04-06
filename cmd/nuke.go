package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ffreis/platform-bootstrap/internal/bootstrap"
)

const errMissingBackendKeys = "%s is missing required backend keys: %s"

var (
	nukeAll            bool
	nukeEnv            string
	nukeBackupDir      string
	bootstrapNukeFn    = bootstrap.Nuke
	nukeRepoRootFn     = bootstrapRepoRoot
	nukeRunStepFn      = runNukeAllStep
	nukePreflightAllFn = preflightBootstrapNukeAll
)

var nukeCmd = &cobra.Command{
	Use:   "nuke",
	Short: "Destroy all Layer 0 bootstrap resources",
	Long: `nuke destroys all resources created by init, in reverse order:

  1. AWS Budget
  2. SNS events topic
  3. IAM platform-admin role (inline policies removed first)
  4. Terraform lock DynamoDB table
  5. Terraform state S3 bucket (all versions emptied first)
  6. Bootstrap registry DynamoDB table

WARNING: This is irreversible. All Terraform state and bootstrap configuration
will be permanently lost.

Pass --dry-run to preview what would be deleted without making any destructive AWS changes.`,

	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		out := newCommandOutput(cmd, deps.ui)

		if nukeAll {
			return runBootstrapNukeAll(ctx, cmd, out)
		}

		repoRoot, err := nukeRepoRootFn()
		if err != nil {
			return err
		}
		backupPlan, err := inspectBootstrapStateStoresForNukeFn(ctx, deps.cfg, deps.clients)
		if err != nil {
			return err
		}
		backupDir := nukeBackupDir
		if backupDir == "" && backupPlan.hasData() {
			backupDir = defaultBootstrapBackupDirForNukeFn(repoRoot)
		}

		if !deps.cfg.DryRun {
			expected := "nuke-" + deps.cfg.OrgName
			if backupPlan.hasData() {
				expected = "backup-nuke-" + deps.cfg.OrgName
			}
			warningTarget := strconv.Quote(deps.cfg.OrgName) + "."
			if deps.ui != nil {
				out.ErrLine(deps.ui.Header("Platform Bootstrap Nuke", "org "+deps.cfg.OrgName))
				out.ErrLine(deps.ui.Badge("warn", "warn") + " This will permanently destroy all Layer 0 bootstrap resources for org " + warningTarget)
			} else {
				out.ErrLine("WARNING: This will permanently destroy all Layer 0 bootstrap resources for org " + warningTarget)
			}
			out.ErrLine("Resources to be deleted:")
			out.ErrLine("  - Budget:          " + deps.cfg.BudgetName())
			out.ErrLine("  - SNS topic:       " + deps.cfg.EventsTopicName())
			out.ErrLine("  - IAM role:        platform-admin")
			out.ErrLine("  - DynamoDB table:  " + deps.cfg.LockTableName())
			out.ErrLine("  - S3 bucket:       " + deps.cfg.StateBucketName())
			out.ErrLine("  - DynamoDB table:  " + deps.cfg.RegistryTableName())
			if backupPlan.hasData() {
				out.ErrLine("Detected stateful data that will be backed up first:")
				for _, line := range backupPlan.summaryLines() {
					out.ErrLine("  - " + line)
				}
				out.ErrLine("Local backup destination:")
				out.ErrLine("  - " + backupDir)
			}
			_, _ = io.WriteString(cmd.ErrOrStderr(), "\nType "+strconv.Quote(expected)+" to confirm: ")

			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			reply := strings.TrimSpace(scanner.Text())
			if reply != expected {
				if deps.ui != nil {
					out.ErrStatus("muted", "skip", "operator confirmation did not match")
				} else {
					out.ErrLine("Cancelled.")
				}
				return nil
			}
		}

		if backupPlan.hasData() && !deps.cfg.DryRun {
			if deps.ui != nil {
				out.Status("info", "backup", "writing local state backup to "+backupDir)
			}
			if err := backupBootstrapStateStoresForNukeFn(ctx, deps.cfg, deps.clients, backupDir, backupPlan); err != nil {
				return &ExitError{Code: exitPartialComplete, Err: err}
			}
			if deps.ui != nil {
				out.Status("ok", "backup", "bootstrap state backup written")
			}
		}

		if err := bootstrapNukeFn(ctx, deps.cfg, deps.clients, cmd.ErrOrStderr()); err != nil {
			return &ExitError{Code: exitPartialComplete, Err: err}
		}

		deps.logger.Info("nuke complete",
			"org", deps.cfg.OrgName,
		)
		if deps.ui != nil {
			out.Status("ok", "ok", "bootstrap resources removed")
		}

		return nil
	},
}

type bootstrapNukeAllStep struct {
	label   string
	workdir string
	command []string
	stdin   string
}

func runBootstrapNukeAll(ctx context.Context, cmd *cobra.Command, out *commandOutput) error {
	repoRoot, err := nukeRepoRootFn()
	if err != nil {
		return err
	}
	platformRoot, orgRepo, baseBackupDir, orgBackupDir, bootstrapBackupDir := resolveBootstrapNukeAllPaths(repoRoot)
	steps := buildBootstrapNukeAllSteps(platformRoot, orgRepo, orgBackupDir)

	bootstrapBackupPlan, err := inspectBootstrapStateStoresForNukeFn(ctx, deps.cfg, deps.clients)
	if err != nil {
		return err
	}
	if err := nukePreflightAllFn(repoRoot, nukeEnv); err != nil {
		return err
	}

	confirmed := confirmBootstrapNukeAllIfNeeded(cmd, out, steps, bootstrapBackupPlan, baseBackupDir)
	if !confirmed {
		return nil
	}

	if err := runBootstrapNukeAllSteps(ctx, cmd, out, steps); err != nil {
		return err
	}

	if err := backupBootstrapNukeAllStateIfNeeded(ctx, out, bootstrapBackupDir, bootstrapBackupPlan); err != nil {
		return err
	}

	if err := bootstrapNukeFn(ctx, deps.cfg, deps.clients, cmd.ErrOrStderr()); err != nil {
		return &ExitError{Code: exitPartialComplete, Err: err}
	}

	deps.logger.Info("nuke-all complete",
		"org", deps.cfg.OrgName,
		"env", nukeEnv,
	)
	if deps.ui != nil {
		out.Status("ok", "ok", "all platform resources removed")
	}
	return nil
}

func resolveBootstrapNukeAllPaths(repoRoot string) (platformRoot, orgRepo, baseBackupDir, orgBackupDir, bootstrapBackupDir string) {
	platformRoot = filepath.Dir(repoRoot)
	orgRepo = filepath.Join(platformRoot, "ffreis-platform-org")
	baseBackupDir = nukeBackupDir
	if baseBackupDir == "" {
		baseBackupDir = filepath.Join(repoRoot, ".backups", "nuke", bootstrapNukeTimestamp())
	}
	orgBackupDir = filepath.Join(baseBackupDir, "platform-org")
	bootstrapBackupDir = filepath.Join(baseBackupDir, "bootstrap")
	return
}

func buildBootstrapNukeAllSteps(platformRoot, orgRepo, orgBackupDir string) []bootstrapNukeAllStep {
	return []bootstrapNukeAllStep{
		{
			label:   "Atlantis",
			workdir: filepath.Join(platformRoot, "ffreis-platform-atlantis"),
			command: []string{"make", "nuke", "ENV=" + nukeEnv},
			stdin:   "nuke-" + nukeEnv + "\n",
		},
		{
			label:   "project-template",
			workdir: filepath.Join(platformRoot, "ffreis-platform-project-template"),
			command: []string{"make", "nuke", "ENV=" + nukeEnv},
			stdin:   "nuke-" + nukeEnv + "\n",
		},
		{
			label:   "github-oidc",
			workdir: filepath.Join(platformRoot, "ffreis-platform-github-oidc"),
			command: []string{"make", "nuke", "ENV=" + nukeEnv},
			stdin:   "nuke-" + nukeEnv + "\n",
		},
		{
			label:   "platform-org build",
			workdir: orgRepo,
			command: []string{"make", "build"},
		},
		{
			label:   "platform-org purge",
			workdir: orgRepo,
			command: []string{"./bin/platform-org", "--env", nukeEnv, "purge", "--force"},
			stdin:   "purge\n",
		},
		{
			label:   "platform-org nuke",
			workdir: orgRepo,
			command: []string{"./bin/platform-org", "--env", nukeEnv, "nuke", "--force", "--backup-dir", orgBackupDir},
			stdin:   "destroy-" + nukeEnv + "\n",
		},
	}
}

func confirmBootstrapNukeAllIfNeeded(cmd *cobra.Command, out *commandOutput, steps []bootstrapNukeAllStep, plan bootstrapStateBackupPlan, baseBackupDir string) bool {
	if deps.cfg.DryRun {
		return true
	}
	return confirmBootstrapNukeAll(cmd, out, steps, plan, baseBackupDir)
}

func runBootstrapNukeAllSteps(ctx context.Context, cmd *cobra.Command, out *commandOutput, steps []bootstrapNukeAllStep) error {
	for _, step := range steps {
		if deps.cfg.DryRun {
			if deps.ui != nil {
				out.Status("muted", "dry-run", step.label+" would run")
			}
			continue
		}
		if deps.ui != nil {
			out.Status("info", "step", step.label)
		}
		if err := nukeRunStepFn(ctx, step, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
			return &ExitError{Code: exitPartialComplete, Err: err}
		}
	}
	return nil
}

func backupBootstrapNukeAllStateIfNeeded(ctx context.Context, out *commandOutput, bootstrapBackupDir string, plan bootstrapStateBackupPlan) error {
	if !plan.hasData() || deps.cfg.DryRun {
		return nil
	}
	if deps.ui != nil {
		out.Status("info", "backup", "writing bootstrap state backup to "+bootstrapBackupDir)
	}
	if err := backupBootstrapStateStoresForNukeFn(ctx, deps.cfg, deps.clients, bootstrapBackupDir, plan); err != nil {
		return &ExitError{Code: exitPartialComplete, Err: err}
	}
	if deps.ui != nil {
		out.Status("ok", "backup", "bootstrap state backup written")
	}
	return nil
}

// confirmBootstrapNukeAll prints the destruction warning, lists steps, and reads operator
// confirmation from stdin. Returns true if the operator confirmed, false if cancelled.
func confirmBootstrapNukeAll(cmd *cobra.Command, out *commandOutput, steps []bootstrapNukeAllStep, plan bootstrapStateBackupPlan, baseBackupDir string) bool {
	expected := "nuke-all-" + deps.cfg.OrgName + "-" + nukeEnv
	warningTarget := strconv.Quote(deps.cfg.OrgName) + "."
	if deps.ui != nil {
		out.ErrLine(deps.ui.Header("Platform Bootstrap Nuke", "org "+deps.cfg.OrgName+" env "+nukeEnv))
		out.ErrLine(deps.ui.Badge("warn", "warn") + " This will permanently destroy ALL platform resources for org " + warningTarget)
	} else {
		out.ErrLine("WARNING: This will permanently destroy ALL platform resources for org " + warningTarget)
	}
	out.ErrLine("Steps to be executed:")
	for i, step := range steps {
		out.ErrLine("  " + strconv.Itoa(i+1) + ". " + step.label)
	}
	out.ErrLine("  " + strconv.Itoa(len(steps)+1) + ". bootstrap Layer 0")
	out.ErrLine("Stateful stores will be backed up before deletion when data exists.")
	out.ErrLine("Backup root:")
	out.ErrLine("  - " + baseBackupDir)
	if plan.hasData() {
		out.ErrLine("Bootstrap stateful data detected:")
		for _, line := range plan.summaryLines() {
			out.ErrLine("  - " + line)
		}
	}
	_, _ = io.WriteString(cmd.ErrOrStderr(), "\nType "+strconv.Quote(expected)+" to confirm: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	reply := strings.TrimSpace(scanner.Text())
	if reply != expected {
		if deps.ui != nil {
			out.ErrStatus("muted", "skip", "operator confirmation did not match")
		} else {
			out.ErrLine("Cancelled.")
		}
		return false
	}
	return true
}

func runNukeAllStep(ctx context.Context, step bootstrapNukeAllStep, stdout, stderr io.Writer) error {
	if len(step.command) == 0 {
		return fmt.Errorf("%s: empty command", step.label)
	}
	cmd := exec.CommandContext(ctx, step.command[0], step.command[1:]...) //nolint:gosec
	cmd.Dir = step.workdir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()
	if step.stdin != "" {
		cmd.Stdin = strings.NewReader(step.stdin)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", step.label, err)
	}
	return nil
}

func bootstrapRepoRoot() (string, error) {
	dir, err := filepath.Abs(".")
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside a git repository (no .git found walking up from %s)", dir)
		}
		dir = parent
	}
}

func bootstrapNukeTimestamp() string {
	return time.Now().UTC().Format("20060102T150405Z")
}

func preflightBootstrapNukeAll(repoRoot, env string) error {
	platformRoot := filepath.Dir(repoRoot)
	checks := []struct {
		label string
		fn    func(string, string) error
	}{
		{label: "Atlantis", fn: preflightAtlantisNuke},
		{label: "project-template", fn: preflightProjectTemplateNuke},
		{label: "github-oidc", fn: preflightGithubOIDCNuke},
		{label: "platform-org", fn: preflightPlatformOrgNuke},
	}
	for _, check := range checks {
		if err := check.fn(platformRoot, env); err != nil {
			return fmt.Errorf("%s preflight failed: %w", check.label, err)
		}
	}
	return nil
}

func preflightAtlantisNuke(platformRoot, env string) error {
	repo := filepath.Join(platformRoot, "ffreis-platform-atlantis")
	if err := requireDir(repo); err != nil {
		return err
	}
	if err := requireDir(filepath.Join(repo, "stack")); err != nil {
		return err
	}
	backendPath := filepath.Join(repo, "envs", env, "backend.hcl")
	backend, err := requireFileText(backendPath)
	if err != nil {
		return err
	}
	if missing := missingBackendKeys(backend, "bucket", "key", "region"); len(missing) > 0 {
		return fmt.Errorf(errMissingBackendKeys, backendPath, strings.Join(missing, ", "))
	}
	return nil
}

func preflightProjectTemplateNuke(platformRoot, env string) error {
	repo := filepath.Join(platformRoot, "ffreis-platform-project-template")
	if err := requireDir(repo); err != nil {
		return err
	}
	if err := requireDir(filepath.Join(repo, "stack")); err != nil {
		return err
	}
	backendPath := filepath.Join(repo, "envs", env, "backend.hcl")
	backend, err := requireFileText(backendPath)
	if err != nil {
		return err
	}
	if strings.Contains(backend, "{ACCOUNT_ID}") {
		return fmt.Errorf("%s still contains placeholder {ACCOUNT_ID}", backendPath)
	}
	if missing := missingBackendKeys(backend, "bucket", "key", "region", "dynamodb_table"); len(missing) > 0 {
		return fmt.Errorf(errMissingBackendKeys, backendPath, strings.Join(missing, ", "))
	}
	if _, err := os.Stat(filepath.Join(repo, "envs", env, "fetched.auto.tfvars.json")); err != nil {
		return fmt.Errorf("missing envs/%s/fetched.auto.tfvars.json; run fetch first", env)
	}
	return nil
}

func preflightGithubOIDCNuke(platformRoot, env string) error {
	repo := filepath.Join(platformRoot, "ffreis-platform-github-oidc")
	if err := requireDir(repo); err != nil {
		return err
	}
	if err := requireDir(filepath.Join(repo, "stack")); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(repo, "envs", env, "config.local.yaml")); err != nil {
		return fmt.Errorf("missing envs/%s/config.local.yaml", env)
	}
	backendPath := filepath.Join(repo, "envs", env, "backend.local.hcl")
	backend, err := requireFileText(backendPath)
	if err != nil {
		return err
	}
	if missing := missingBackendKeys(backend, "bucket", "key", "region", "dynamodb_table"); len(missing) > 0 {
		return fmt.Errorf(errMissingBackendKeys, backendPath, strings.Join(missing, ", "))
	}
	return nil
}

func preflightPlatformOrgNuke(platformRoot, env string) error {
	repo := filepath.Join(platformRoot, "ffreis-platform-org")
	if err := requireDir(repo); err != nil {
		return err
	}
	if err := requireDir(filepath.Join(repo, "terraform", "stack")); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(repo, "terraform", "envs", env, "terraform.tfvars")); err != nil {
		return fmt.Errorf("missing terraform/envs/%s/terraform.tfvars", env)
	}
	if _, err := os.Stat(filepath.Join(repo, "terraform", "envs", env, "fetched.auto.tfvars.json")); err != nil {
		return fmt.Errorf("missing terraform/envs/%s/fetched.auto.tfvars.json; run fetch/apply setup first", env)
	}
	return nil
}

func requireDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func requireFileText(path string) (string, error) {
	//nolint:gosec // path is derived from known internal config locations, not user input
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func missingBackendKeys(text string, keys ...string) []string {
	missing := make([]string, 0, len(keys))
	lines := strings.Split(text, "\n")
	for _, key := range keys {
		found := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"=") {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, key)
		}
	}
	return missing
}

func init() {
	nukeCmd.Flags().BoolVar(&nukeAll, "all", false, "Destroy all platform stacks in reverse dependency order, including org purge and bootstrap Layer 0")
	nukeCmd.Flags().StringVar(&nukeEnv, "env", "prod", "Environment to destroy when --all is set")
	nukeCmd.Flags().StringVar(&nukeBackupDir, "backup-dir", "", "Directory for local state backups before deletion (defaults under the repo when data exists)")
	rootCmd.AddCommand(nukeCmd)
}
