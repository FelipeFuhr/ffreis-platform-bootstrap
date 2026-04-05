package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/bootstrap"
)

const platformAdminRoleName = "platform-admin"

func formatErr(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ReplaceAll(err.Error(), "\n", " ")
	return strings.TrimSpace(msg)
}

type bootstrapDoctorCheck struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Detail   string   `json:"detail"`
	Hint     string   `json:"hint,omitempty"`
	Related  []string `json:"related,omitempty"`
	Blocking bool     `json:"blocking"`
}

type bootstrapDoctorSection struct {
	Title  string                 `json:"title"`
	Checks []bootstrapDoctorCheck `json:"checks"`
}

type BootstrapDoctorReport struct {
	Mode     string                   `json:"mode"`
	Sections []bootstrapDoctorSection `json:"sections"`
	Summary  bootstrapDoctorSummary   `json:"summary"`
}

type bootstrapDoctorSummary struct {
	OK    int `json:"ok"`
	Warn  int `json:"warn"`
	Fail  int `json:"fail"`
	Info  int `json:"info"`
	Total int `json:"total"`
}

type bootstrapDoctorMode struct {
	Name               string
	IncludePermissions bool
	IncludeResources   bool
	IncludeRegistry    bool
	IncludeContract    bool
	IncludeTrust       bool
	RequireExisting    bool
}

var (
	bootstrapDoctorRunFn = runBootstrapDoctor
	bootstrapDoctorModes = struct {
		command bootstrapDoctorMode
		audit   bootstrapDoctorMode
		init    bootstrapDoctorMode
	}{
		command: bootstrapDoctorMode{
			Name:               "doctor",
			IncludePermissions: true,
			IncludeResources:   true,
			IncludeRegistry:    true,
			IncludeContract:    true,
			IncludeTrust:       true,
			RequireExisting:    true,
		},
		audit: bootstrapDoctorMode{
			Name:               "audit",
			IncludePermissions: false,
			IncludeResources:   true,
			IncludeRegistry:    true,
			IncludeContract:    true,
			IncludeTrust:       true,
			RequireExisting:    true,
		},
		init: bootstrapDoctorMode{
			Name:               "init-preflight",
			IncludePermissions: true,
			IncludeResources:   false,
			IncludeRegistry:    false,
			IncludeContract:    true,
			IncludeTrust:       true,
			RequireExisting:    false,
		},
	}
)

func runBootstrapDoctor(ctx context.Context, mode bootstrapDoctorMode) (BootstrapDoctorReport, error) {
	report := BootstrapDoctorReport{Mode: mode.Name}
	if mode.IncludePermissions {
		report.Sections = append(report.Sections, bootstrapPermissionSection(ctx))
	}
	if mode.IncludeResources {
		report.Sections = append(report.Sections, bootstrapResourceSection(ctx, mode.RequireExisting))
	}
	if mode.IncludeRegistry {
		section, err := bootstrapRegistrySection(ctx, mode.RequireExisting)
		if err != nil {
			return BootstrapDoctorReport{}, err
		}
		report.Sections = append(report.Sections, section)
	}
	if mode.IncludeContract || mode.IncludeTrust {
		section, err := bootstrapContractSection(ctx, mode)
		if err != nil {
			return BootstrapDoctorReport{}, err
		}
		report.Sections = append(report.Sections, section)
	}
	report.Summary = summarizeBootstrapDoctor(report.Sections)
	return report, nil
}

func summarizeBootstrapDoctor(sections []bootstrapDoctorSection) bootstrapDoctorSummary {
	var summary bootstrapDoctorSummary
	for _, section := range sections {
		for _, check := range section.Checks {
			summary.Total++
			switch check.Status {
			case "ok":
				summary.OK++
			case "warn":
				summary.Warn++
			case "fail":
				summary.Fail++
			case "info":
				summary.Info++
			}
		}
	}
	return summary
}

func (r BootstrapDoctorReport) HasFailures() bool {
	return r.Summary.Fail > 0
}

func bootstrapPermissionSection(ctx context.Context) bootstrapDoctorSection {
	checks := []struct {
		key     string
		title   string
		hint    string
		run     func() error
		related []string
	}{
		{
			key:   "iam:get-account-summary",
			title: "Validate IAM read access",
			hint:  "ensure the current credentials can call IAM APIs in the management account",
			run: func() error {
				_, err := deps.clients.IAM.GetAccountSummary(ctx, &iam.GetAccountSummaryInput{})
				return err
			},
		},
		{
			key:   "s3:list-buckets",
			title: "Validate S3 access",
			hint:  "ensure the current credentials can inspect the root tfstate bucket",
			run: func() error {
				_, err := deps.clients.S3.ListBuckets(ctx, &s3.ListBucketsInput{})
				return err
			},
		},
		{
			key:   "dynamodb:list-tables",
			title: "Validate DynamoDB access",
			hint:  "ensure the current credentials can inspect the registry and lock tables",
			run: func() error {
				_, err := deps.clients.DynamoDB.ListTables(ctx, &dynamodb.ListTablesInput{Limit: aws.Int32(1)})
				return err
			},
		},
		{
			key:   "sns:list-topics",
			title: "Validate SNS access",
			hint:  "ensure the current credentials can inspect the platform events topic",
			run: func() error {
				_, err := deps.clients.SNS.ListTopics(ctx, &sns.ListTopicsInput{})
				return err
			},
		},
		{
			key:   "budgets:describe-budgets",
			title: "Validate Budgets access",
			hint:  "ensure the current credentials can inspect the bootstrap budget",
			run: func() error {
				_, err := deps.clients.Budgets.DescribeBudgets(ctx, &budgets.DescribeBudgetsInput{
					AccountId:  aws.String(deps.clients.AccountID),
					MaxResults: aws.Int32(1),
				})
				return err
			},
		},
	}

	results := make([]bootstrapDoctorCheck, 0, len(checks))
	for _, check := range checks {
		err := check.run()
		status := "ok"
		detail := "read-only access confirmed"
		if err != nil {
			status = "fail"
			detail = formatErr(err)
		}
		results = append(results, bootstrapDoctorCheck{
			Key:      check.key,
			Title:    check.title,
			Status:   status,
			Detail:   detail,
			Hint:     check.hint,
			Related:  check.related,
			Blocking: status == "fail",
		})
	}

	return bootstrapDoctorSection{Title: "Permissions", Checks: results}
}

func bootstrapResourceSection(ctx context.Context, requireExisting bool) bootstrapDoctorSection {
	type resourceCheck struct {
		key          string
		title        string
		resourceType string
		name         string
		exists       func() (bool, error)
	}

	checks := []resourceCheck{
		{
			key:          "resource.registry-table",
			title:        "Bootstrap registry table is readable",
			resourceType: "DynamoDBTable",
			name:         deps.cfg.RegistryTableName(),
			exists: func() (bool, error) {
				return deps.clients.TableExistsChecked(ctx, deps.cfg.RegistryTableName())
			},
		},
		{
			key:          "resource.state-bucket",
			title:        "Root tfstate bucket is readable",
			resourceType: "S3Bucket",
			name:         deps.cfg.StateBucketName(),
			exists: func() (bool, error) {
				return deps.clients.BucketExistsChecked(ctx, deps.cfg.StateBucketName())
			},
		},
		{
			key:          "resource.lock-table",
			title:        "Root tf lock table is readable",
			resourceType: "DynamoDBTable",
			name:         deps.cfg.LockTableName(),
			exists: func() (bool, error) {
				return deps.clients.TableExistsChecked(ctx, deps.cfg.LockTableName())
			},
		},
		{
			key:          "resource.platform-admin-role",
			title:        "platform-admin role is readable",
			resourceType: "IAMRole",
			name:         platformAdminRoleName,
			exists: func() (bool, error) {
				return deps.clients.RoleExistsChecked(ctx, platformAdminRoleName)
			},
		},
		{
			key:          "resource.events-topic",
			title:        "Platform events topic is readable",
			resourceType: "SNSTopic",
			name:         deps.cfg.EventsTopicName(),
			exists: func() (bool, error) {
				return deps.clients.TopicExistsChecked(ctx, deps.cfg.EventsTopicName())
			},
		},
		{
			key:          "resource.monthly-budget",
			title:        "Bootstrap budget is readable",
			resourceType: "AWSBudget",
			name:         deps.cfg.BudgetName(),
			exists: func() (bool, error) {
				return deps.clients.BudgetExistsChecked(ctx, deps.cfg.BudgetName())
			},
		},
	}

	results := make([]bootstrapDoctorCheck, 0, len(checks))
	for _, check := range checks {
		exists, err := check.exists()
		status := "ok"
		detail := fmt.Sprintf("%s %s is present", check.resourceType, check.name)
		if err != nil {
			status = "fail"
			detail = formatErr(err)
		} else if !exists {
			if requireExisting {
				status = "fail"
				detail = fmt.Sprintf("%s %s is missing", check.resourceType, check.name)
			} else {
				status = "info"
				detail = fmt.Sprintf("%s %s is not created yet", check.resourceType, check.name)
			}
		}
		results = append(results, bootstrapDoctorCheck{
			Key:      check.key,
			Title:    check.title,
			Status:   status,
			Detail:   detail,
			Hint:     "run platform-bootstrap init to reconcile Layer 0 resources",
			Related:  []string{check.resourceType + "/" + check.name},
			Blocking: status == "fail",
		})
	}

	return bootstrapDoctorSection{Title: "Layer 0 Resources", Checks: results}
}

func bootstrapRegistrySection(ctx context.Context, requireExisting bool) (bootstrapDoctorSection, error) {
	records, err := platformaws.ScanRegistry(ctx, deps.clients.DynamoDB, deps.cfg.RegistryTableName())
	if err != nil {
		return bootstrapDoctorSection{}, fmt.Errorf("scan bootstrap registry: %w", err)
	}

	var checks []bootstrapDoctorCheck
	seen := map[string]platformaws.RegistryRecord{}
	duplicateKeys := map[string]bool{}
	for _, rec := range records {
		key := rec.ResourceType + "/" + rec.ResourceName
		if prev, ok := seen[key]; ok {
			if prev.CreatedBy != rec.CreatedBy || !prev.CreatedAt.Equal(rec.CreatedAt) || prev.Tags != rec.Tags {
				duplicateKeys[key] = true
			}
			continue
		}
		seen[key] = rec
	}

	if len(duplicateKeys) == 0 {
		checks = append(checks, bootstrapDoctorCheck{
			Key:      "registry.duplicates",
			Title:    "Registry keys are unique",
			Status:   "ok",
			Detail:   "no conflicting duplicate registry rows found",
			Hint:     "remove duplicate registry rows if they appear",
			Blocking: false,
		})
	} else {
		keys := make([]string, 0, len(duplicateKeys))
		for key := range duplicateKeys {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		checks = append(checks, bootstrapDoctorCheck{
			Key:      "registry.duplicates",
			Title:    "Registry keys are unique",
			Status:   "fail",
			Detail:   "conflicting duplicate registry rows: " + strings.Join(keys, ", "),
			Hint:     "repair the bootstrap registry so each resource key has exactly one row",
			Related:  keys,
			Blocking: true,
		})
	}

	expected := bootstrap.ExpectedResources(deps.cfg)
	for _, resource := range expected {
		key := resource.ResourceType + "/" + resource.ResourceName
		_, registered := seen[key]
		exists := deps.clients.ResourceExists(ctx, resource.ResourceType, resource.ResourceName)

		status, detail := registryResourceStatus(registered, exists, requireExisting)
		checks = append(checks, bootstrapDoctorCheck{
			Key:      "registry." + strings.ToLower(resource.ResourceType) + "." + resource.ResourceName,
			Title:    fmt.Sprintf("%s registry contract", resource.ResourceName),
			Status:   status,
			Detail:   detail,
			Hint:     "re-run platform-bootstrap init to recreate or re-register the expected bootstrap resource",
			Related:  []string{key},
			Blocking: status == "fail",
		})
	}

	return bootstrapDoctorSection{Title: "Registry Integrity", Checks: checks}, nil
}

// registryResourceStatus determines the doctor check status and detail message for a single
// expected resource by comparing whether it is registered in the registry and exists in AWS.
func registryResourceStatus(registered, exists, requireExisting bool) (status, detail string) {
	switch {
	case !registered && exists:
		return "fail", "resource exists in AWS but is missing from the bootstrap registry"
	case !registered && !exists && requireExisting:
		return "fail", "resource and registry row are both missing"
	case !registered && !exists:
		return "info", "resource and registry row are not created yet"
	case registered && !exists && requireExisting:
		return "fail", "registry row exists but the live resource is missing"
	default:
		return "ok", "registry row and live resource match"
	}
}

func bootstrapContractSection(ctx context.Context, mode bootstrapDoctorMode) (bootstrapDoctorSection, error) {
	checks := []bootstrapDoctorCheck{
		{
			Key:      "contract.backend-export",
			Title:    "Exported backend contract is internally consistent",
			Status:   "ok",
			Detail:   fmt.Sprintf("bucket=%s  table=%s  region=%s", deps.cfg.StateBucketName(), deps.cfg.LockTableName(), deps.cfg.StateRegion),
			Hint:     "ensure bootstrap fetch writes the same root backend values expected by platform-org",
			Related:  []string{deps.cfg.StateBucketName(), deps.cfg.LockTableName()},
			Blocking: false,
		},
		{
			Key:      "contract.backend-render",
			Title:    "Rendered backend export contains the current root contract",
			Status:   "ok",
			Detail:   "backend.local.hcl export uses the current root bucket, lock table, and region",
			Hint:     "repair bootstrap fetch output generation if the exported backend contract drifts",
			Related:  []string{deps.cfg.StateBucketName(), deps.cfg.LockTableName()},
			Blocking: false,
		},
	}

	rendered := renderBackendHCL(backendConfig{
		Bucket:        deps.cfg.StateBucketName(),
		DynamoDBTable: deps.cfg.LockTableName(),
		Region:        deps.cfg.Region,
	})
	if !strings.Contains(rendered, strconvQuote(deps.cfg.StateBucketName())) ||
		!strings.Contains(rendered, strconvQuote(deps.cfg.LockTableName())) ||
		!strings.Contains(rendered, strconvQuote(deps.cfg.Region)) {
		checks[1].Status = "fail"
		checks[1].Detail = "rendered backend export does not include the current root bucket/table/region"
		checks[1].Blocking = true
	}

	if mode.IncludeTrust {
		checks = append(checks, bootstrapTrustCheck(ctx, mode.RequireExisting))
	}

	return bootstrapDoctorSection{Title: "Exported Contract", Checks: checks}, nil
}

func bootstrapTrustCheck(ctx context.Context, requireExisting bool) bootstrapDoctorCheck {
	check := bootstrapDoctorCheck{
		Key:      "contract.platform-admin-trust",
		Title:    "platform-admin trust allows the account root principal",
		Status:   "ok",
		Hint:     "re-run platform-bootstrap init to repair the platform-admin trust policy",
		Related:  []string{"IAMRole/platform-admin"},
		Blocking: false,
	}

	out, err := deps.clients.IAM.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(platformAdminRoleName)})
	if err != nil {
		exists, existsErr := deps.clients.RoleExistsChecked(ctx, platformAdminRoleName)
		if existsErr != nil {
			check.Status = "fail"
			check.Detail = formatErr(existsErr)
			check.Blocking = true
			return check
		}
		if !exists {
			if requireExisting {
				check.Status = "fail"
				check.Detail = "platform-admin role is missing"
				check.Blocking = true
			} else {
				check.Status = "info"
				check.Detail = "platform-admin role is not created yet"
			}
			return check
		}
		check.Status = "fail"
		check.Detail = formatErr(err)
		check.Blocking = true
		return check
	}

	rawDoc := aws.ToString(out.Role.AssumeRolePolicyDocument)
	decodedDoc, decodeErr := url.QueryUnescape(rawDoc)
	if decodeErr != nil {
		check.Status = "fail"
		check.Detail = fmt.Sprintf("could not decode trust policy: %v", decodeErr)
		check.Blocking = true
		return check
	}
	expectedPrincipal := fmt.Sprintf(platformaws.IAMRootPrincipalARNFormat, deps.clients.AccountID)
	if strings.Contains(decodedDoc, expectedPrincipal) {
		check.Detail = "platform-admin trust includes the current account root principal"
		return check
	}

	var policyDoc map[string]any
	if err := json.Unmarshal([]byte(decodedDoc), &policyDoc); err != nil {
		check.Status = "fail"
		check.Detail = fmt.Sprintf("could not parse trust policy JSON: %v", err)
		check.Blocking = true
		return check
	}

	check.Status = "fail"
	check.Detail = "platform-admin trust does not include the current account root principal"
	check.Blocking = true
	return check
}

func printBootstrapDoctorReport(out *commandOutput, report BootstrapDoctorReport) {
	for idx, section := range report.Sections {
		if idx > 0 {
			out.Blank()
		}
		out.Header(section.Title, "")
		rows := make([][]string, 0, len(section.Checks))
		for _, check := range section.Checks {
			hint := check.Hint
			if strings.TrimSpace(hint) == "" {
				hint = "-"
			}
			rows = append(rows, []string{
				bootstrapDoctorStatusIcon(check.Status),
				check.Title,
				check.Detail,
				hint,
			})
		}
		_ = out.Table([]string{"STATUS", "CHECK", "DETAIL", "HINT"}, rows)
	}
}

func printBootstrapDoctorSummary(out *commandOutput, report BootstrapDoctorReport) {
	out.Summary("Integrity Summary",
		countPart("ok", report.Summary.OK),
		countPart("warn", report.Summary.Warn),
		countPart("fail", report.Summary.Fail),
		countPart("info", report.Summary.Info),
	)
}

func bootstrapDoctorStatusIcon(status string) string {
	switch status {
	case "ok":
		if deps.ui != nil {
			return deps.ui.Badge("ok", "ok")
		}
		return "OK  "
	case "warn":
		if deps.ui != nil {
			return deps.ui.Badge("warn", "warn")
		}
		return "WARN"
	case "fail":
		if deps.ui != nil {
			return deps.ui.Badge("error", "fail")
		}
		return "FAIL"
	case "info":
		if deps.ui != nil {
			return deps.ui.Badge("info", "info")
		}
		return "INFO"
	default:
		return status
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
