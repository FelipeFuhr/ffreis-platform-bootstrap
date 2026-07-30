package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
)

// fetchedConfig is the JSON structure written to the output file.
// It matches the Terraform variable types in platform-org's variables.tf.
// aws_profile is intentionally omitted — it is static config that belongs in
// terraform.tfvars (committed), not here. The platform-org CLI injects
// credentials via AWS_ACCESS_KEY_ID env vars, making the profile irrelevant.
type fetchedConfig struct {
	Org      string                       `json:"org"`
	Accounts map[string]map[string]string `json:"accounts"`

	// BudgetAlertEmail carries only the first recipient. Deprecated: kept
	// populated so consumers that still declare the singular
	// `budget_alert_email` variable keep working. Drop it once every consumer
	// reads budget_alert_emails.
	BudgetAlertEmail string `json:"budget_alert_email,omitempty"`

	// BudgetAlertEmails is the full recipient list and the value consumers
	// should read. Omitted entirely when the registry holds no alert address.
	BudgetAlertEmails []string `json:"budget_alert_emails,omitempty"`
}

// backendConfig holds the values written to backend.local.hcl.
type backendConfig struct {
	Bucket        string
	DynamoDBTable string
	Region        string
}

// writeFetchedConfig writes fetched.auto.tfvars.json to outputPath and,
// if backendOutputPath is non-empty, writes backend.local.hcl there too.
func writeFetchedConfig(outputPath, backendOutputPath string) error {
	ctx := rootCmd.Context()
	tableName := deps.cfg.RegistryTableName()

	accounts, err := fetchAccountsConfig(ctx, tableName)
	if err != nil {
		return err
	}

	budgetAlertEmails, err := fetchAdminAlertEmails(ctx, tableName)
	if err != nil {
		return err
	}

	out := fetchedConfig{
		Org:               deps.cfg.OrgName,
		Accounts:          accounts,
		BudgetAlertEmail:  primaryAlertEmail(budgetAlertEmails),
		BudgetAlertEmails: budgetAlertEmails,
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling output: %w", err)
	}
	data = append(data, '\n')

	if err := writeFetchedJSON(outputPath, data, len(accounts)); err != nil {
		return err
	}

	if backendOutputPath != "" {
		bcfg := backendConfig{
			Bucket:        deps.cfg.StateBucketName(),
			DynamoDBTable: deps.cfg.LockTableName(),
			Region:        deps.cfg.Region,
		}
		if err := writeBackendHCL(backendOutputPath, bcfg); err != nil {
			return err
		}
	}

	return nil
}

func fetchAccountsConfig(ctx context.Context, tableName string) (map[string]map[string]string, error) {
	records, err := platformaws.FetchConfig(ctx, deps.clients.DynamoDB, tableName, "account")
	if err != nil {
		return nil, fmt.Errorf("fetching account config: %w", err)
	}
	return accountsFromRecords(records), nil
}

func fetchAdminAlertEmails(ctx context.Context, tableName string) ([]string, error) {
	records, err := platformaws.FetchConfig(ctx, deps.clients.DynamoDB, tableName, "admin")
	if err != nil {
		return nil, fmt.Errorf("fetching admin config: %w", err)
	}
	return adminAlertEmails(records), nil
}

func accountsFromRecords(records []platformaws.ConfigRecord) map[string]map[string]string {
	accounts := make(map[string]map[string]string, len(records))
	for _, rec := range records {
		accounts[rec.ConfigName] = rec.Data
	}
	return accounts
}

// adminAlertEmails returns every budget alert recipient held in the registry
// under CONFIG#admin / alert_email. Returns nil when the record is absent or
// holds no usable address.
func adminAlertEmails(records []platformaws.ConfigRecord) []string {
	for _, rec := range records {
		if rec.ConfigName == "alert_email" {
			return splitAlertEmails(rec.Data["email"])
		}
	}
	return nil
}

// splitAlertEmails parses the registry's single `email` string field, which
// holds one or more recipients separated by commas. Entries are trimmed and
// blanks dropped, so "a@x.com, b@x.com," yields exactly two addresses and a
// lone "a@x.com" yields one. Returns nil rather than an empty slice so the
// omitempty JSON tags drop the field entirely when nothing is configured.
func splitAlertEmails(raw string) []string {
	parts := strings.Split(raw, ",")
	emails := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			emails = append(emails, trimmed)
		}
	}
	if len(emails) == 0 {
		return nil
	}
	return emails
}

// primaryAlertEmail returns the first recipient, or "" when there are none.
// It backs the deprecated singular budget_alert_email field.
func primaryAlertEmail(emails []string) string {
	if len(emails) == 0 {
		return ""
	}
	return emails[0]
}

func writeFetchedJSON(outputPath string, data []byte, accountCount int) error {
	if outputPath == "-" {
		if _, err := os.Stdout.Write(data); err != nil {
			return fmt.Errorf("writing to stdout: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0750); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0600); err != nil {
		return fmt.Errorf("writing output to %s: %w", outputPath, err)
	}
	if deps.logger != nil {
		deps.logger.Info("wrote tfvars", "path", outputPath, "accounts", accountCount)
	}
	return nil
}

func renderBackendHCL(cfg backendConfig) string {
	var b strings.Builder
	b.WriteString("# Generated by platform-bootstrap fetch - do not commit (gitignored).\n")
	b.WriteString("# Contains real infrastructure identifiers for the root Terraform state.\n")
	b.WriteString("# The state key lives in envs/prod/backend.hcl and IS committed.\n")
	b.WriteString("#\n")
	b.WriteString("# Usage (from terraform/stack/):\n")
	b.WriteString("#   terraform init \\\n")
	b.WriteString("#     -backend-config=backend.local.hcl \\\n")
	b.WriteString("#     -backend-config=../envs/prod/backend.hcl\n")
	b.WriteString("bucket         = ")
	b.WriteString(strconv.Quote(cfg.Bucket))
	b.WriteString("\n")
	b.WriteString("dynamodb_table = ")
	b.WriteString(strconv.Quote(cfg.DynamoDBTable))
	b.WriteString("\n")
	b.WriteString("region         = ")
	b.WriteString(strconv.Quote(cfg.Region))
	b.WriteString("\n")
	return b.String()
}

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch platform config from the registry and write Terraform config files",
	Long: `fetch reads configuration stored in the bootstrap registry DynamoDB table
and writes the files that Terraform needs to init and apply platform-org.

Output files:
  --output       fetched.auto.tfvars.json — auto-loaded by Terraform, gitignored
  --backend-out  backend.local.hcl       — S3 backend config for terraform init, gitignored

Both files are gitignored by design — always derived from the registry.

Example:
  platform-bootstrap fetch \
    --org ffreis \
    --profile bootstrap \
    --output    ../your-platform-org-repo/terraform/envs/prod/fetched.auto.tfvars.json \
    --backend-out ../your-platform-org-repo/terraform/stack/backend.local.hcl`,

	RunE: func(cmd *cobra.Command, _ []string) error {
		outputPath, _ := cmd.Flags().GetString("output")
		backendOutputPath, _ := cmd.Flags().GetString("backend-out")

		deps.logger.Info("fetching platform config",
			"table", deps.cfg.RegistryTableName(),
			"output", outputPath,
			"backend_out", backendOutputPath,
		)

		if err := writeFetchedConfig(outputPath, backendOutputPath); err != nil {
			return &ExitError{Code: exitAWSError, Err: err}
		}

		deps.logger.Info("fetch complete", "output", outputPath, "backend_out", backendOutputPath)
		return nil
	},
}

// writeBackendHCL renders backend.local.hcl to path.
func writeBackendHCL(path string, cfg backendConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("creating backend output directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(renderBackendHCL(cfg)), 0600); err != nil {
		return fmt.Errorf("writing backend.local.hcl to %s: %w", path, err)
	}
	if deps.logger != nil {
		deps.logger.Info("wrote backend config", "path", path)
	}
	return nil
}

func init() {
	fetchCmd.Flags().String("output", "-",
		`path to write fetched.auto.tfvars.json; use "-" for stdout`)
	fetchCmd.Flags().String("backend-out", "",
		`path to write backend.local.hcl (e.g. ../your-platform-org-repo/terraform/stack/backend.local.hcl); omit to skip`)
	rootCmd.AddCommand(fetchCmd)
}
