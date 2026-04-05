package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/budgets"
	budgetstypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
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
	// Status is "ok", "missing", "owned", or "unmanaged".
	Status string `json:"status"`
	// Expected reports whether the resource is part of bootstrap's canonical
	// managed inventory for this org.
	Expected bool `json:"expected,omitempty"`
	// Owner is the owning stack/tool identity when known.
	Owner string `json:"owner,omitempty"`
}

// AuditReport is the full audit output.
type AuditReport struct {
	OrgName   string                 `json:"org"`
	AccountID string                 `json:"account_id"`
	Region    string                 `json:"region"`
	Resources []AuditResult          `json:"resources"`
	Summary   AuditSummary           `json:"summary"`
	Integrity *BootstrapDoctorReport `json:"integrity,omitempty"`
}

// AuditSummary aggregates counts across the report.
type AuditSummary struct {
	Total     int `json:"total"`
	OK        int `json:"ok"`
	Missing   int `json:"missing"`
	Owned     int `json:"owned"`
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
		recordByKey := make(map[string]platformaws.RegistryRecord, len(records))
		for _, rec := range records {
			key := rec.ResourceType + "/" + rec.ResourceName
			recordByKey[key] = rec
		}

		// --- 3. Emit the full expected inventory ---
		seenKeys := make(map[string]bool, len(records))
		for _, e := range bootstrap.ExpectedResources(deps.cfg) {
			key := e.ResourceType + "/" + e.ResourceName
			rec, registered := recordByKey[key]
			exists := deps.clients.ResourceExists(ctx, e.ResourceType, e.ResourceName)

			status := "missing"
			if exists && registered {
				status = "ok"
			} else if exists {
				status = "unmanaged"
			}

			result := AuditResult{
				ResourceType: e.ResourceType,
				ResourceName: e.ResourceName,
				Status:       status,
				Expected:     true,
				Owner:        "bootstrap",
			}
			if registered {
				result.CreatedAt = rec.CreatedAt
				result.CreatedBy = rec.CreatedBy
			}
			results = append(results, result)
			seenKeys[key] = true
		}

		// --- 4. Detect unexpected bootstrap-like resources ---
		discovered, err := discoverBootstrapLikeResources(ctx)
		if err != nil {
			return &ExitError{Code: exitAWSError, Err: fmt.Errorf("discovering unmanaged resources: %w", err)}
		}
		for _, discoveredResource := range discovered {
			key := discoveredResource.ResourceType + "/" + discoveredResource.ResourceName
			if seenKeys[key] {
				continue
			}
			status := "unmanaged"
			if discoveredResource.Owner != "" && discoveredResource.Owner != "bootstrap" {
				status = "owned"
			}
			results = append(results, AuditResult{
				ResourceType: discoveredResource.ResourceType,
				ResourceName: discoveredResource.ResourceName,
				Status:       status,
				Expected:     false,
				Owner:        discoveredResource.Owner,
			})
			seenKeys[key] = true
		}

		expectedOrder := make(map[string]int)
		for i, e := range bootstrap.ExpectedResources(deps.cfg) {
			expectedOrder[e.ResourceType+"/"+e.ResourceName] = i
		}
		sortAuditResults(results, expectedOrder)

		// --- 5. Compute summary ---
		summary := AuditSummary{Total: len(results)}
		for _, r := range results {
			switch r.Status {
			case "ok":
				summary.OK++
			case "missing":
				summary.Missing++
			case "owned":
				summary.Owned++
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

		doctorReport, err := bootstrapDoctorRunFn(ctx, bootstrapDoctorModes.audit)
		if err != nil {
			return &ExitError{Code: exitAWSError, Err: fmt.Errorf("running integrity checks: %w", err)}
		}
		report.Integrity = &doctorReport

		// --- 6. Output ---
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
		if doctorReport.HasFailures() {
			return &ExitError{
				Code: exitPartialComplete,
				Err:  fmt.Errorf("audit integrity: %d blocking check(s) failed", doctorReport.Summary.Fail),
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

	expected, unexpected := splitBootstrapAuditResults(r.Resources)
	printBootstrapAuditSection(out, "Expected Bootstrap Resources", expected)
	out.Blank()
	printBootstrapAuditSection(out, "Unexpected Bootstrap-like Resources", unexpected)

	if r.Integrity != nil {
		out.Blank()
		printBootstrapDoctorReport(out, *r.Integrity)
		out.Blank()
		printBootstrapDoctorSummary(out, *r.Integrity)
	}

	if deps.ui != nil {
		out.Blank()
		out.Summary("Summary",
			countPart("total", r.Summary.Total),
			countPart("ok", r.Summary.OK),
			countPart("missing", r.Summary.Missing),
			countPart("owned", r.Summary.Owned),
			countPart("unmanaged", r.Summary.Unmanaged),
		)
		return
	}
	out.Blank()
	out.Line("Summary: " + strconv.Itoa(r.Summary.Total) + " total, " +
		strconv.Itoa(r.Summary.OK) + " ok, " +
		strconv.Itoa(r.Summary.Missing) + " missing, " +
		strconv.Itoa(r.Summary.Owned) + " owned, " +
		strconv.Itoa(r.Summary.Unmanaged) + " unmanaged")
}

func splitBootstrapAuditResults(results []AuditResult) (expected []AuditResult, unexpected []AuditResult) {
	for _, res := range results {
		if res.Expected {
			expected = append(expected, res)
		} else {
			unexpected = append(unexpected, res)
		}
	}
	return expected, unexpected
}

func printBootstrapAuditSection(out *commandOutput, title string, resources []AuditResult) {
	out.Header(title, "")

	rows := make([][]string, 0, len(resources))
	for _, res := range resources {
		createdAt := "-"
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
			displayOwner(res.Owner),
			createdAt,
			createdBy,
		})
	}
	_ = out.Table([]string{"STATUS", "TYPE", "NAME", "OWNER", "CREATED AT", "CREATED BY"}, rows)
}

func sortAuditResults(results []AuditResult, expectedOrder map[string]int) {
	sort.Slice(results, func(i, j int) bool {
		leftKey := results[i].ResourceType + "/" + results[i].ResourceName
		rightKey := results[j].ResourceType + "/" + results[j].ResourceName
		leftExpectedIndex, leftExpected := expectedOrder[leftKey]
		rightExpectedIndex, rightExpected := expectedOrder[rightKey]

		if leftExpected != rightExpected {
			return leftExpected
		}
		if leftExpected {
			leftRank := bootstrapStatusRank(results[i].Status)
			rightRank := bootstrapStatusRank(results[j].Status)
			if leftRank != rightRank {
				return leftRank < rightRank
			}
			return leftExpectedIndex < rightExpectedIndex
		}
		if results[i].ResourceType != results[j].ResourceType {
			return results[i].ResourceType < results[j].ResourceType
		}
		return results[i].ResourceName < results[j].ResourceName
	})
}

func bootstrapStatusRank(status string) int {
	switch status {
	case "ok":
		return 0
	case "unmanaged":
		return 1
	case "owned":
		return 2
	case "missing":
		return 3
	default:
		return 4
	}
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
	case "owned":
		if deps.ui != nil {
			return deps.ui.Badge("muted", "owned")
		}
		return "OWNED   "
	}
	return s
}

func displayOwner(owner string) string {
	if strings.TrimSpace(owner) == "" {
		return "-"
	}
	return owner
}

func init() {
	auditCmd.Flags().Bool("json", false, "output audit report as JSON")
	rootCmd.AddCommand(auditCmd)
}

type discoveredBootstrapResource struct {
	ResourceType string
	ResourceName string
	Owner        string
}

func discoverBootstrapLikeResources(ctx context.Context) ([]discoveredBootstrapResource, error) {
	var resources []discoveredBootstrapResource

	bucketsOut, err := deps.clients.S3.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("listing S3 buckets: %w", err)
	}
	for _, bucket := range bucketsOut.Buckets {
		name := strings.TrimSpace(stringValue(bucket.Name))
		if matchesBootstrapManagedName("S3Bucket", name) {
			resources = append(resources, discoveredBootstrapResource{
				ResourceType: "S3Bucket",
				ResourceName: name,
				Owner:        bucketOwner(ctx, name),
			})
		}
	}

	var tableStart *string
	for {
		tablesOut, err := deps.clients.DynamoDB.ListTables(ctx, &dynamodb.ListTablesInput{ExclusiveStartTableName: tableStart})
		if err != nil {
			return nil, fmt.Errorf("listing DynamoDB tables: %w", err)
		}
		for _, table := range tablesOut.TableNames {
			if matchesBootstrapManagedName("DynamoDBTable", table) {
				resources = append(resources, discoveredBootstrapResource{
					ResourceType: "DynamoDBTable",
					ResourceName: table,
					Owner:        dynamoTableOwner(ctx, table),
				})
			}
		}
		if tablesOut.LastEvaluatedTableName == nil || *tablesOut.LastEvaluatedTableName == "" {
			break
		}
		tableStart = tablesOut.LastEvaluatedTableName
	}

	var nextTopicToken *string
	for {
		topicsOut, err := deps.clients.SNS.ListTopics(ctx, &sns.ListTopicsInput{NextToken: nextTopicToken})
		if err != nil {
			return nil, fmt.Errorf("listing SNS topics: %w", err)
		}
		for _, topic := range topicsOut.Topics {
			arn := stringValue(topic.TopicArn)
			name := topicNameFromARN(arn)
			if matchesBootstrapManagedName("SNSTopic", name) {
				resources = append(resources, discoveredBootstrapResource{
					ResourceType: "SNSTopic",
					ResourceName: name,
					Owner:        snsTopicOwner(ctx, arn),
				})
			}
		}
		if topicsOut.NextToken == nil || *topicsOut.NextToken == "" {
			break
		}
		nextTopicToken = topicsOut.NextToken
	}

	var nextBudgetToken *string
	for {
		budgetsOut, err := deps.clients.Budgets.DescribeBudgets(ctx, &budgets.DescribeBudgetsInput{
			AccountId:  &deps.clients.AccountID,
			NextToken:  nextBudgetToken,
			MaxResults: nil,
		})
		if err != nil {
			return nil, fmt.Errorf("listing budgets: %w", err)
		}
		for _, budget := range budgetsOut.Budgets {
			name := strings.TrimSpace(stringValue(budget.BudgetName))
			if matchesBootstrapManagedName("AWSBudget", name) {
				resources = append(resources, discoveredBootstrapResource{
					ResourceType: "AWSBudget",
					ResourceName: name,
					Owner:        budgetOwner(ctx, name),
				})
			}
		}
		if budgetsOut.NextToken == nil || *budgetsOut.NextToken == "" {
			break
		}
		nextBudgetToken = budgetsOut.NextToken
	}

	return resources, nil
}

type s3TagReader interface {
	GetBucketTagging(context.Context, *s3.GetBucketTaggingInput, ...func(*s3.Options)) (*s3.GetBucketTaggingOutput, error)
}

type dynamoTagReader interface {
	ListTagsOfResource(context.Context, *dynamodb.ListTagsOfResourceInput, ...func(*dynamodb.Options)) (*dynamodb.ListTagsOfResourceOutput, error)
}

type snsTagReader interface {
	ListTagsForResource(context.Context, *sns.ListTagsForResourceInput, ...func(*sns.Options)) (*sns.ListTagsForResourceOutput, error)
}

type budgetsTagReader interface {
	ListTagsForResource(context.Context, *budgets.ListTagsForResourceInput, ...func(*budgets.Options)) (*budgets.ListTagsForResourceOutput, error)
}

func bucketOwner(ctx context.Context, bucket string) string {
	reader, ok := deps.clients.S3.(s3TagReader)
	if !ok {
		return ""
	}
	out, err := reader.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: &bucket})
	if err != nil {
		return ""
	}
	return ownerFromS3Tags(out.TagSet)
}

func dynamoTableOwner(ctx context.Context, table string) string {
	desc, err := deps.clients.DynamoDB.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: &table})
	if err != nil || desc.Table == nil || desc.Table.TableArn == nil {
		return ""
	}
	reader, ok := deps.clients.DynamoDB.(dynamoTagReader)
	if !ok {
		return ""
	}
	out, err := reader.ListTagsOfResource(ctx, &dynamodb.ListTagsOfResourceInput{ResourceArn: desc.Table.TableArn})
	if err != nil {
		return ""
	}
	return ownerFromDynamoTags(out.Tags)
}

func snsTopicOwner(ctx context.Context, topicARN string) string {
	if topicARN == "" {
		return ""
	}
	reader, ok := deps.clients.SNS.(snsTagReader)
	if !ok {
		return ""
	}
	out, err := reader.ListTagsForResource(ctx, &sns.ListTagsForResourceInput{ResourceArn: &topicARN})
	if err != nil {
		return ""
	}
	return ownerFromSNSTags(out.Tags)
}

func budgetOwner(ctx context.Context, budgetName string) string {
	reader, ok := deps.clients.Budgets.(budgetsTagReader)
	if !ok {
		return ""
	}
	arn := fmt.Sprintf("arn:aws:budgets::%s:budget/%s", deps.clients.AccountID, budgetName)
	out, err := reader.ListTagsForResource(ctx, &budgets.ListTagsForResourceInput{ResourceARN: &arn})
	if err != nil {
		var notFound *budgetstypes.NotFoundException
		if errors.As(err, &notFound) {
			return ""
		}
		return ""
	}
	return ownerFromBudgetTags(out.ResourceTags)
}

func ownerFromS3Tags(tags []s3types.Tag) string {
	values := make(map[string]string, len(tags))
	for _, tag := range tags {
		values[stringValue(tag.Key)] = stringValue(tag.Value)
	}
	return ownerFromTagMap(values)
}

func ownerFromDynamoTags(tags []dbtypes.Tag) string {
	values := make(map[string]string, len(tags))
	for _, tag := range tags {
		values[stringValue(tag.Key)] = stringValue(tag.Value)
	}
	return ownerFromTagMap(values)
}

func ownerFromSNSTags(tags []snstypes.Tag) string {
	values := make(map[string]string, len(tags))
	for _, tag := range tags {
		values[stringValue(tag.Key)] = stringValue(tag.Value)
	}
	return ownerFromTagMap(values)
}

func ownerFromBudgetTags(tags []budgetstypes.ResourceTag) string {
	values := make(map[string]string, len(tags))
	for _, tag := range tags {
		values[stringValue(tag.Key)] = stringValue(tag.Value)
	}
	return ownerFromTagMap(values)
}

func ownerFromTagMap(tags map[string]string) string {
	if stack := strings.TrimSpace(tags["Stack"]); stack != "" {
		return stack
	}
	if managedBy := strings.TrimSpace(tags["ManagedBy"]); managedBy != "" {
		return managedBy
	}
	if layer := strings.TrimSpace(tags["Layer"]); layer != "" {
		return layer
	}
	return ""
}

func matchesBootstrapManagedName(resourceType, name string) bool {
	if name == "" {
		return false
	}

	orgPrefix := deps.cfg.OrgName + "-"
	switch resourceType {
	case "S3Bucket", "DynamoDBTable", "SNSTopic", "AWSBudget":
		return strings.HasPrefix(name, orgPrefix)
	default:
		return false
	}
}

func topicNameFromARN(arn string) string {
	if arn == "" {
		return ""
	}
	parts := strings.Split(arn, ":")
	return parts[len(parts)-1]
}

func stringValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}
