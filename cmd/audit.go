package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/bootstrap"
)

// AuditResult is the structured result of a single audited resource.
type AuditResult struct {
	ResourceType string    `json:"resource_type"`
	ResourceName string    `json:"resource_name"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	CreatedBy    string    `json:"created_by,omitempty"`
	// Status is "ok", "missing", or "unmanaged".
	Status string `json:"status"`
}

// AuditReport is the full audit output.
type AuditReport struct {
	OrgName   string        `json:"org"`
	AccountID string        `json:"account_id"`
	Region    string        `json:"region"`
	Resources []AuditResult `json:"resources"`
	Summary   AuditSummary  `json:"summary"`
}

// AuditSummary aggregates counts across the report.
type AuditSummary struct {
	Total     int `json:"total"`
	OK        int `json:"ok"`
	Missing   int `json:"missing"`
	Unmanaged int `json:"unmanaged"`
}

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit managed resources against the registry",
	Long: `audit scans the bootstrap registry table and verifies that every
registered resource still exists in AWS.

It also checks every resource that bootstrap would create for this org
and flags any that exist in AWS but are absent from the registry.

Exit codes:
  0  all resources healthy
  3  inconsistencies detected (missing or unmanaged resources)`,

	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		jsonOut, _ := cmd.Flags().GetBool("json")

		registryTable := deps.cfg.RegistryTableName()

		// --- 1. Load the registry ---
		records, err := platformaws.ScanRegistry(ctx, deps.clients.DynamoDB, registryTable)
		if err != nil {
			return &ExitError{Code: exitAWSError, Err: fmt.Errorf("scanning registry: %w", err)}
		}

		results := make([]AuditResult, 0, len(records)+8)

		// --- 2. Check each registered resource ---
		registeredKeys := make(map[string]bool)
		for _, rec := range records {
			key := rec.ResourceType + "/" + rec.ResourceName
			registeredKeys[key] = true

			status := "ok"
			if !deps.clients.ResourceExists(ctx, rec.ResourceType, rec.ResourceName) {
				status = "missing"
			}

			results = append(results, AuditResult{
				ResourceType: rec.ResourceType,
				ResourceName: rec.ResourceName,
				CreatedAt:    rec.CreatedAt,
				CreatedBy:    rec.CreatedBy,
				Status:       status,
			})
		}

		// --- 3. Detect unmanaged resources ---
		// Walk every resource bootstrap would manage. If it exists in AWS
		// but is absent from the registry, it is "unmanaged".
		for _, e := range bootstrap.ExpectedResources(deps.cfg) {
			key := e.ResourceType + "/" + e.ResourceName
			if registeredKeys[key] {
				continue // already covered above
			}
			if deps.clients.ResourceExists(ctx, e.ResourceType, e.ResourceName) {
				results = append(results, AuditResult{
					ResourceType: e.ResourceType,
					ResourceName: e.ResourceName,
					Status:       "unmanaged",
				})
			}
		}

		// Sort for stable output: type then name.
		sort.Slice(results, func(i, j int) bool {
			if results[i].ResourceType != results[j].ResourceType {
				return results[i].ResourceType < results[j].ResourceType
			}
			return results[i].ResourceName < results[j].ResourceName
		})

		// --- 4. Compute summary ---
		summary := AuditSummary{Total: len(results)}
		for _, r := range results {
			switch r.Status {
			case "ok":
				summary.OK++
			case "missing":
				summary.Missing++
			case "unmanaged":
				summary.Unmanaged++
			}
		}

		report := AuditReport{
			OrgName:   deps.cfg.OrgName,
			AccountID: deps.clients.AccountID,
			Region:    deps.clients.Region,
			Resources: results,
			Summary:   summary,
		}

		// --- 5. Output ---
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				return &ExitError{Code: exitUserError, Err: err}
			}
		} else {
			printAuditReport(cmd, report)
		}

		if summary.Missing > 0 || summary.Unmanaged > 0 {
			return &ExitError{
				Code: exitPartialComplete,
				Err: fmt.Errorf("audit: %d missing, %d unmanaged resource(s)",
					summary.Missing, summary.Unmanaged),
			}
		}

		return nil
	},
}

// printAuditReport writes a human-readable audit table to stdout.
func printAuditReport(cmd *cobra.Command, r AuditReport) {
	out := newCommandOutput(cmd, deps.ui)
	if deps.ui != nil {
		out.Header("Platform Bootstrap Audit", auditSummary(r.OrgName, r.AccountID, r.Region))
		out.Blank()
	} else {
		out.Line("Audit report — org: " + r.OrgName + "  account: " + r.AccountID + "  region: " + r.Region)
		out.Blank()
	}

	rows := make([][]string, 0, len(r.Resources))
	for _, res := range r.Resources {
		createdAt := ""
		if !res.CreatedAt.IsZero() {
			createdAt = res.CreatedAt.UTC().Format(time.RFC3339)
		}
		createdBy := res.CreatedBy
		if createdBy == "" {
			createdBy = "-"
		}
		rows = append(rows, []string{
			statusIcon(res.Status),
			res.ResourceType,
			res.ResourceName,
			createdAt,
			createdBy,
		})
	}
	_ = out.Table([]string{"STATUS", "TYPE", "NAME", "CREATED AT", "CREATED BY"}, rows)

	if deps.ui != nil {
		out.Blank()
		out.Summary("Summary",
			countPart("total", r.Summary.Total),
			countPart("ok", r.Summary.OK),
			countPart("missing", r.Summary.Missing),
			countPart("unmanaged", r.Summary.Unmanaged),
		)
		return
	}
	out.Blank()
	out.Line(fmt.Sprintf("Summary: %d total, %d ok, %d missing, %d unmanaged",
		r.Summary.Total, r.Summary.OK, r.Summary.Missing, r.Summary.Unmanaged))
}

func statusIcon(s string) string {
	switch s {
	case "ok":
		if deps.ui != nil {
			return deps.ui.Badge("ok", "ok")
		}
		return "OK      "
	case "missing":
		if deps.ui != nil {
			return deps.ui.Badge("error", "missing")
		}
		return "MISSING "
	case "unmanaged":
		if deps.ui != nil {
			return deps.ui.Badge("warn", "unmanaged")
		}
		return "UNMANAGED"
	}
	return s
}

func init() {
	auditCmd.Flags().Bool("json", false, "output audit report as JSON")
	rootCmd.AddCommand(auditCmd)
}
