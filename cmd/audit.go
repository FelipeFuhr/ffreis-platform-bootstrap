package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
	"github.com/spf13/cobra"
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

// expectedResource describes a resource that bootstrap would create for this org.
type expectedResource struct {
	resourceType string
	resourceName string
}

// expectedResources returns the complete set of platform resources for this org.
func expectedResources(cfg *config.Config) []expectedResource {
	return []expectedResource{
		{"DynamoDBTable", cfg.RegistryTableName()},
		{"S3Bucket", cfg.StateBucketName()},
		{"DynamoDBTable", cfg.LockTableName()},
		{"IAMRole", config.RoleNamePlatformAdmin},
		{"SNSTopic", cfg.EventsTopicName()},
		{"AWSBudget", cfg.BudgetName()},
	}
}

// checkExists returns true if the named resource of the given type exists in AWS.
func checkExists(ctx context.Context, clients *platformaws.Clients, resourceType, resourceName string) bool {
	switch resourceType {
	case "S3Bucket":
		return clients.BucketExists(ctx, resourceName)
	case "DynamoDBTable":
		return clients.TableExists(ctx, resourceName)
	case "IAMRole":
		return clients.RoleExists(ctx, resourceName)
	case "SNSTopic":
		return clients.TopicExists(ctx, resourceName)
	case "AWSBudget":
		return clients.BudgetExists(ctx, resourceName)
	}
	return false
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
			if !checkExists(ctx, deps.clients, rec.ResourceType, rec.ResourceName) {
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
		for _, e := range expectedResources(deps.cfg) {
			key := e.resourceType + "/" + e.resourceName
			if registeredKeys[key] {
				continue // already covered above
			}
			if checkExists(ctx, deps.clients, e.resourceType, e.resourceName) {
				results = append(results, AuditResult{
					ResourceType: e.resourceType,
					ResourceName: e.resourceName,
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
			printAuditReport(report)
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
func printAuditReport(r AuditReport) {
	fmt.Printf("Audit report — org: %s  account: %s  region: %s\n\n",
		r.OrgName, r.AccountID, r.Region)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tTYPE\tNAME\tCREATED AT\tCREATED BY")
	fmt.Fprintln(w, "------\t----\t----\t----------\t----------")

	for _, res := range r.Resources {
		createdAt := ""
		if !res.CreatedAt.IsZero() {
			createdAt = res.CreatedAt.UTC().Format(time.RFC3339)
		}
		createdBy := res.CreatedBy
		if createdBy == "" {
			createdBy = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			statusIcon(res.Status), res.ResourceType, res.ResourceName, createdAt, createdBy)
	}
	w.Flush()

	fmt.Printf("\nSummary: %d total, %d ok, %d missing, %d unmanaged\n",
		r.Summary.Total, r.Summary.OK, r.Summary.Missing, r.Summary.Unmanaged)
}

func statusIcon(s string) string {
	switch s {
	case "ok":
		return "OK      "
	case "missing":
		return "MISSING "
	case "unmanaged":
		return "UNMANAGED"
	}
	return s
}

func init() {
	auditCmd.Flags().Bool("json", false, "output audit report as JSON")
	rootCmd.AddCommand(auditCmd)
}
