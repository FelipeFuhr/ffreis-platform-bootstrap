package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/spf13/cobra"
)

// fetchedConfig is the JSON structure written to the output file.
// It matches the Terraform variable types in platform-org's variables.tf.
type fetchedConfig struct {
	Org              string                       `json:"org"`
	Accounts         map[string]map[string]string `json:"accounts"`
	BudgetAlertEmail string                       `json:"budget_alert_email,omitempty"`
}

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch platform config from the registry and write a Terraform tfvars file",
	Long: `fetch reads configuration stored in the bootstrap registry DynamoDB table
and writes a JSON file that Terraform auto-loads as variable values.

The output file provides:
  - org:      the organisation identifier
  - accounts: map of account name → {email} for all registered member accounts

Run this after 'platform-bootstrap init' to populate the file, then run
'terraform plan' in platform-org. The output file is gitignored by design —
it is always derived from the registry, never committed.

Example:
  platform-bootstrap fetch \
    --org ffreis \
    --profile bootstrap \
    --output ../platform-org/envs/prod/fetched.auto.tfvars.json`,

	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		outputPath, _ := cmd.Flags().GetString("output")

		tableName := deps.cfg.RegistryTableName()
		deps.logger.Info("fetching platform config",
			"table", tableName,
			"output", outputPath,
		)

		records, err := platformaws.FetchConfig(ctx, deps.clients.DynamoDB, tableName, "account")
		if err != nil {
			return &ExitError{Code: exitAWSError, Err: fmt.Errorf("fetching account config: %w", err)}
		}

		if len(records) == 0 {
			return &ExitError{
				Code: exitUserError,
				Err: fmt.Errorf(
					"no account config found in registry table %q — "+
						"run 'platform-bootstrap init --account name:email ...' first",
					tableName,
				),
			}
		}

		accounts := make(map[string]map[string]string, len(records))
		for _, rec := range records {
			accounts[rec.ConfigName] = rec.Data
		}

		adminRecords, err := platformaws.FetchConfig(ctx, deps.clients.DynamoDB, tableName, "admin")
		if err != nil {
			return &ExitError{Code: exitAWSError, Err: fmt.Errorf("fetching admin config: %w", err)}
		}
		var budgetAlertEmail string
		for _, rec := range adminRecords {
			if rec.ConfigName == "alert_email" {
				budgetAlertEmail = rec.Data["email"]
				break
			}
		}

		out := fetchedConfig{
			Org:              deps.cfg.OrgName,
			Accounts:         accounts,
			BudgetAlertEmail: budgetAlertEmail,
		}

		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return &ExitError{Code: exitUserError, Err: fmt.Errorf("marshalling output: %w", err)}
		}
		data = append(data, '\n')

		if outputPath == "-" {
			_, err = fmt.Fprint(os.Stdout, string(data))
		} else {
			err = os.WriteFile(outputPath, data, 0600)
		}
		if err != nil {
			return &ExitError{Code: exitUserError, Err: fmt.Errorf("writing output to %s: %w", outputPath, err)}
		}

		deps.logger.Info("fetch complete",
			"accounts", len(records),
			"output", outputPath,
		)
		return nil
	},
}

func init() {
	fetchCmd.Flags().String("output", "-",
		`path to write the JSON tfvars file; use "-" for stdout`)
	rootCmd.AddCommand(fetchCmd)
}
