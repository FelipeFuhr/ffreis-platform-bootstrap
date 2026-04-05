package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
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
	"github.com/ffreis/platform-bootstrap/internal/config"
	platformui "github.com/ffreis/platform-bootstrap/internal/ui"
)

const (
	testBootstrapRoleARN  = "arn:aws:iam::123456789012:role/bootstrap"
	testBootstrapRepoRoot = "/tmp/platform/ffreis-platform-bootstrap"
	testOrgStackName      = "platform-org"
	errUnexpected         = "unexpected error: %v"
	errUnexpectedText     = "unexpected error text: %v"
	errUnexpectedUI       = "ui.New() unexpected error: %v"
	errUnexpectedRunE     = "RunE() unexpected error: %v"
	errOutputMissing      = "output missing %q in:\n%s"
	errFlagsSet           = "Flags().Set() unexpected error: %v"
	errCreateTemp         = "CreateTemp() unexpected error: %v"
	errWriteString        = "WriteString() unexpected error: %v"
	errSeek               = "Seek() unexpected error: %v"
	errStdoutMissingOK    = "stdout missing success status in:\n%s"
	errNukeCallCount      = "bootstrapNukeFn called %d times; want 1"
	flagBackendOut        = "backend-out"
)

func TestInitPreRunERequiresRootEmail(t *testing.T) {
	cfg := testConfig()
	cfg.RootEmail = ""
	setTestDeps(t, cfg, &platformaws.Clients{}, nil)

	cmd, _, _ := newTestCommand(context.Background())
	err := initCmd.PreRunE(cmd, nil)
	if err == nil {
		t.Fatal("expected PreRunE to fail")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitUserError {
		t.Fatalf(errUnexpected, err)
	}
	if !strings.Contains(err.Error(), "root email is required") {
		t.Fatalf(errUnexpectedText, err)
	}
}

func TestInitRunEDryRun(t *testing.T) {
	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf(errUnexpectedUI, err)
	}

	cfg := testConfig()
	cfg.DryRun = true
	clients := &platformaws.Clients{AccountID: "123456789012", CallerARN: "arn:aws:iam::123456789012:root", Region: cfg.Region}
	setTestDeps(t, cfg, clients, presenter)

	cmd, stdout, _ := newTestCommand(testCommandContext(presenter))
	cmd.Flags().String("org-dir", "", "")
	oldDoctor := bootstrapDoctorRunFn
	t.Cleanup(func() { bootstrapDoctorRunFn = oldDoctor })
	bootstrapDoctorRunFn = func(context.Context, bootstrapDoctorMode) (BootstrapDoctorReport, error) {
		return BootstrapDoctorReport{Summary: bootstrapDoctorSummary{OK: 1, Total: 1}}, nil
	}

	if err := initCmd.RunE(cmd, nil); err != nil {
		t.Fatalf(errUnexpectedRunE, err)
	}

	got := stdout.String()
	for _, want := range []string{"Platform Bootstrap Init", "Config:", "dry-run=true", "Outputs:"} {
		if !strings.Contains(got, want) {
			t.Fatalf(errOutputMissing, want, got)
		}
	}
}

func TestAuditRunEJSONAndInconsistencies(t *testing.T) {
	cfg := testConfig()
	record, err := platformaws.NewRegistryRecord("S3Bucket", cfg.StateBucketName(), testBootstrapRoleARN, map[string]string{"ManagedBy": "test"})
	if err != nil {
		t.Fatalf("NewRegistryRecord() unexpected error: %v", err)
	}
	record.CreatedAt = time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	registryItem, err := attributevalue.MarshalMap(record)
	if err != nil {
		t.Fatalf("MarshalMap() unexpected error: %v", err)
	}

	clients := &platformaws.Clients{
		AccountID: "123456789012",
		CallerARN: testBootstrapRoleARN,
		Region:    cfg.Region,
		DynamoDB: &cmdDynamoDBMock{
			scanFn: func(in *dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
				return &dynamodb.ScanOutput{Items: []map[string]dbtypes.AttributeValue{registryItem}}, nil
			},
			describeTableFn: func(in *dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
				name := *in.TableName
				if name == cfg.RegistryTableName() {
					return &dynamodb.DescribeTableOutput{Table: &dbtypes.TableDescription{TableStatus: dbtypes.TableStatusActive}}, nil
				}
				return nil, resourceNotFoundTable()
			},
		},
		S3: &cmdS3Mock{
			headBucketFn: func(in *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				return nil, &s3types.NotFound{}
			},
		},
		IAM: &cmdIAMMock{},
		SNS: &cmdSNSMock{
			getTopicAttributesFn: func(*sns.GetTopicAttributesInput) (*sns.GetTopicAttributesOutput, error) {
				return nil, &snstypes.NotFoundException{}
			},
		},
		Budgets: &cmdBudgetsMock{
			describeBudgetFn: func(*budgets.DescribeBudgetInput) (*budgets.DescribeBudgetOutput, error) {
				return nil, &budgetstypes.NotFoundException{}
			},
		},
	}
	setTestDeps(t, cfg, clients, nil)

	cmd, _, _ := newTestCommand(context.Background())
	cmd.Flags().Bool("json", false, "")
	oldDoctor := bootstrapDoctorRunFn
	t.Cleanup(func() { bootstrapDoctorRunFn = oldDoctor })
	bootstrapDoctorRunFn = func(context.Context, bootstrapDoctorMode) (BootstrapDoctorReport, error) {
		return BootstrapDoctorReport{Summary: bootstrapDoctorSummary{OK: 1, Total: 1}}, nil
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf(errFlagsSet, err)
	}

	jsonOut := captureStdout(t, func() {
		err = auditCmd.RunE(cmd, nil)
	})
	if err == nil {
		t.Fatal("expected audit to report inconsistencies")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitPartialComplete {
		t.Fatalf(errUnexpected, err)
	}

	var report AuditReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("json.Unmarshal() unexpected error: %v", err)
	}
	if report.Summary.Missing == 0 || report.Summary.Unmanaged == 0 {
		t.Fatalf("expected missing and unmanaged resources in report: %+v", report.Summary)
	}
}

func TestAuditRunEJSONClassifiesDiscoveredOwnedResources(t *testing.T) {
	cfg := testConfig()

	clients := &platformaws.Clients{
		AccountID: "123456789012",
		CallerARN: testBootstrapRoleARN,
		Region:    cfg.Region,
		DynamoDB: &cmdDynamoDBMock{
			scanFn: func(in *dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
				return &dynamodb.ScanOutput{}, nil
			},
			describeTableFn: func(in *dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
				tableName := *in.TableName
				return &dynamodb.DescribeTableOutput{
					Table: &dbtypes.TableDescription{
						TableArn: &[]string{"arn:aws:dynamodb:us-east-1:123456789012:table/" + tableName}[0],
					},
				}, nil
			},
			listTagsFn: func(in *dynamodb.ListTagsOfResourceInput) (*dynamodb.ListTagsOfResourceOutput, error) {
				return &dynamodb.ListTagsOfResourceOutput{
					Tags: []dbtypes.Tag{
						{Key: &[]string{"ManagedBy"}[0], Value: &[]string{"terraform"}[0]},
						{Key: &[]string{"Stack"}[0], Value: &[]string{testOrgStackName}[0]},
					},
				}, nil
			},
			listTablesOut: &dynamodb.ListTablesOutput{
				TableNames: []string{
					cfg.RegistryTableName(),
					cfg.OrgName + "-runtime-extra",
				},
			},
		},
		S3: &cmdS3Mock{
			headBucketFn: func(in *s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				return nil, &s3types.NotFound{}
			},
			getTaggingFn: func(in *s3.GetBucketTaggingInput) (*s3.GetBucketTaggingOutput, error) {
				return &s3.GetBucketTaggingOutput{
					TagSet: []s3types.Tag{
						{Key: &[]string{"ManagedBy"}[0], Value: &[]string{"terraform"}[0]},
						{Key: &[]string{"Stack"}[0], Value: &[]string{testOrgStackName}[0]},
					},
				}, nil
			},
			listBucketsOut: &s3.ListBucketsOutput{
				Buckets: []s3types.Bucket{
					{Name: &[]string{cfg.OrgName + "-manual-bucket"}[0]},
				},
			},
		},
		IAM: &cmdIAMMock{},
		SNS: &cmdSNSMock{
			getTopicAttributesFn: func(*sns.GetTopicAttributesInput) (*sns.GetTopicAttributesOutput, error) {
				return nil, &snstypes.NotFoundException{}
			},
			listTagsFn: func(*sns.ListTagsForResourceInput) (*sns.ListTagsForResourceOutput, error) {
				return &sns.ListTagsForResourceOutput{
					Tags: []snstypes.Tag{
						{Key: &[]string{"ManagedBy"}[0], Value: &[]string{"terraform"}[0]},
						{Key: &[]string{"Stack"}[0], Value: &[]string{testOrgStackName}[0]},
					},
				}, nil
			},
			listTopicsOut: &sns.ListTopicsOutput{
				Topics: []snstypes.Topic{
					{TopicArn: &[]string{"arn:aws:sns:us-east-1:123456789012:" + cfg.OrgName + "-manual-topic"}[0]},
				},
			},
		},
		Budgets: &cmdBudgetsMock{
			describeBudgetFn: func(*budgets.DescribeBudgetInput) (*budgets.DescribeBudgetOutput, error) {
				return nil, &budgetstypes.NotFoundException{}
			},
			listTagsFn: func(*budgets.ListTagsForResourceInput) (*budgets.ListTagsForResourceOutput, error) {
				return &budgets.ListTagsForResourceOutput{
					ResourceTags: []budgetstypes.ResourceTag{
						{Key: &[]string{"ManagedBy"}[0], Value: &[]string{"terraform"}[0]},
						{Key: &[]string{"Stack"}[0], Value: &[]string{testOrgStackName}[0]},
					},
				}, nil
			},
			describeBudgetsOut: &budgets.DescribeBudgetsOutput{
				Budgets: []budgetstypes.Budget{
					{BudgetName: &[]string{cfg.OrgName + "-manual-budget"}[0]},
				},
			},
		},
	}
	setTestDeps(t, cfg, clients, nil)

	cmd, _, _ := newTestCommand(context.Background())
	cmd.Flags().Bool("json", false, "")
	oldDoctor := bootstrapDoctorRunFn
	t.Cleanup(func() { bootstrapDoctorRunFn = oldDoctor })
	bootstrapDoctorRunFn = func(context.Context, bootstrapDoctorMode) (BootstrapDoctorReport, error) {
		return BootstrapDoctorReport{Summary: bootstrapDoctorSummary{OK: 1, Total: 1}}, nil
	}
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf(errFlagsSet, err)
	}

	var runErr error
	jsonOut := captureStdout(t, func() {
		runErr = auditCmd.RunE(cmd, nil)
	})
	if runErr == nil {
		t.Fatal("expected audit to report missing expected resources")
	}

	var report AuditReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("json.Unmarshal() unexpected error: %v", err)
	}

	want := map[string]bool{
		"S3Bucket/" + cfg.OrgName + "-manual-bucket":      false,
		"DynamoDBTable/" + cfg.OrgName + "-runtime-extra": false,
		"SNSTopic/" + cfg.OrgName + "-manual-topic":       false,
		"AWSBudget/" + cfg.OrgName + "-manual-budget":     false,
	}
	for _, resource := range report.Resources {
		key := resource.ResourceType + "/" + resource.ResourceName
		if _, ok := want[key]; ok && resource.Status == "owned" && resource.Owner == testOrgStackName {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Fatalf("expected discovered owned resource %q in report: %+v", key, report.Resources)
		}
	}
}

func TestAuditRunEScanError(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		DynamoDB: &cmdDynamoDBMock{
			scanFn: func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
				return nil, errors.New("scan boom")
			},
		},
	}
	setTestDeps(t, cfg, clients, nil)

	cmd, _, _ := newTestCommand(context.Background())
	cmd.Flags().Bool("json", false, "")

	err := auditCmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected audit to fail")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitAWSError {
		t.Fatalf(errUnexpected, err)
	}
	if !strings.Contains(err.Error(), "scanning registry") {
		t.Fatalf(errUnexpectedText, err)
	}
}

func TestPrintAuditReportAndStatusIcon(t *testing.T) {
	cfg := testConfig()
	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf(errUnexpectedUI, err)
	}
	setTestDeps(t, cfg, &platformaws.Clients{}, presenter)

	cmd, stdout, _ := newTestCommand(context.Background())
	printAuditReport(cmd, AuditReport{
		OrgName:   cfg.OrgName,
		AccountID: "123456789012",
		Region:    cfg.Region,
		Resources: []AuditResult{
			{ResourceType: "S3Bucket", ResourceName: "bucket", Status: "ok", Expected: true, Owner: "bootstrap"},
			{ResourceType: "SNSTopic", ResourceName: "manual-topic", Status: "owned", Owner: testOrgStackName},
		},
		Summary: AuditSummary{Total: 2, OK: 1, Owned: 1},
	})

	got := stdout.String()
	for _, want := range []string{
		"Platform Bootstrap Audit",
		"Expected Bootstrap Resources",
		"Unexpected Bootstrap-like Resources",
		"STATUS",
		"Summary:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf(errOutputMissing, want, got)
		}
	}
	if got := statusIcon("unknown"); got != "unknown" {
		t.Fatalf("statusIcon() = %q", got)
	}
}

func TestDoctorRunESuccess(t *testing.T) {
	cfg := testConfig()
	cfg.AWSProfile = "bootstrap"
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		CallerARN: testBootstrapRoleARN,
		Region:    cfg.Region,
		IAM:       &cmdIAMMock{},
		S3:        &cmdS3Mock{},
		DynamoDB:  &cmdDynamoDBMock{},
		SNS:       &cmdSNSMock{},
		Budgets:   &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	cmd, stdout, _ := newTestCommand(context.Background())
	oldDoctor := bootstrapDoctorRunFn
	t.Cleanup(func() { bootstrapDoctorRunFn = oldDoctor })
	bootstrapDoctorRunFn = func(context.Context, bootstrapDoctorMode) (BootstrapDoctorReport, error) {
		return BootstrapDoctorReport{
			Mode: "doctor",
			Sections: []bootstrapDoctorSection{{
				Title: "Layer 0",
				Checks: []bootstrapDoctorCheck{{
					Key:    "layer0.bucket",
					Title:  "root state bucket exists",
					Status: "ok",
					Detail: "bucket present",
				}},
			}},
			Summary: bootstrapDoctorSummary{OK: 1, Total: 1},
		}, nil
	}
	if err := doctorCmd.RunE(cmd, nil); err != nil {
		t.Fatalf(errUnexpectedRunE, err)
	}

	got := stdout.String()
	for _, want := range []string{"platform-bootstrap doctor", "Layer 0", "root state bucket exists", "Integrity Summary:", "ok=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf(errOutputMissing, want, got)
		}
	}
}

func TestDoctorRunEFailureIncludesHints(t *testing.T) {
	cfg := testConfig()
	cfg.AWSProfile = "bootstrap"
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		CallerARN: testBootstrapRoleARN,
		Region:    cfg.Region,
		IAM:       &cmdIAMMock{getAccountSummaryErr: errors.New("denied\nline2")},
		S3:        &cmdS3Mock{},
		DynamoDB:  &cmdDynamoDBMock{},
		SNS:       &cmdSNSMock{},
		Budgets:   &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	cmd, stdout, _ := newTestCommand(context.Background())
	oldDoctor := bootstrapDoctorRunFn
	t.Cleanup(func() { bootstrapDoctorRunFn = oldDoctor })
	bootstrapDoctorRunFn = func(context.Context, bootstrapDoctorMode) (BootstrapDoctorReport, error) {
		return BootstrapDoctorReport{
			Mode: "doctor",
			Sections: []bootstrapDoctorSection{{
				Title: "Permissions",
				Checks: []bootstrapDoctorCheck{{
					Key:      "iam:get-account-summary",
					Title:    "Validate IAM read access",
					Status:   "fail",
					Detail:   "denied line2",
					Hint:     "aws sso login --profile bootstrap",
					Blocking: true,
				}},
			}},
			Summary: bootstrapDoctorSummary{Fail: 1, Total: 1},
		}, nil
	}
	err := doctorCmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected doctor to fail")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitPartialComplete {
		t.Fatalf(errUnexpected, err)
	}
	got := stdout.String()
	for _, want := range []string{"doctor failed: 1 integrity check(s) failed", "aws sso login --profile bootstrap", "denied line2", "Integrity Summary:", "fail=1"} {
		if !strings.Contains(got+err.Error(), want) {
			t.Fatalf("missing %q in output/error:\nOUTPUT:\n%s\nERR:%v", want, got, err)
		}
	}
}

func TestFormatErr(t *testing.T) {
	if got := formatErr(nil); got != "" {
		t.Fatalf("formatErr(nil) = %q", got)
	}
	if got := formatErr(errors.New("line1\n line2 ")); got != "line1  line2" {
		t.Fatalf("formatErr() = %q", got)
	}
}

func TestNukeRunECancelledByConfirmation(t *testing.T) {
	cfg := testConfig()
	cfg.DryRun = false
	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf(errUnexpectedUI, err)
	}
	setTestDeps(t, cfg, &platformaws.Clients{}, presenter)

	oldStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = oldStdin })
	inputFile, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf(errCreateTemp, err)
	}
	if _, err := inputFile.WriteString("nope\n"); err != nil {
		t.Fatalf(errWriteString, err)
	}
	if _, err := inputFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf(errSeek, err)
	}
	os.Stdin = inputFile

	oldInspect := inspectBootstrapStateStoresForNukeFn
	oldBackup := backupBootstrapStateStoresForNukeFn
	t.Cleanup(func() {
		inspectBootstrapStateStoresForNukeFn = oldInspect
		backupBootstrapStateStoresForNukeFn = oldBackup
	})
	inspectBootstrapStateStoresForNukeFn = func(context.Context, *config.Config, *platformaws.Clients) (bootstrapStateBackupPlan, error) {
		return bootstrapStateBackupPlan{}, nil
	}
	backupBootstrapStateStoresForNukeFn = func(context.Context, *config.Config, *platformaws.Clients, string, bootstrapStateBackupPlan) error {
		t.Fatal("backup should not run on cancel")
		return nil
	}

	cmd, _, stderr := newTestCommand(context.Background())
	if err := nukeCmd.RunE(cmd, nil); err != nil {
		t.Fatalf(errUnexpectedRunE, err)
	}
	got := stderr.String()
	for _, want := range []string{"Platform Bootstrap Nuke", "Resources to be deleted:", "[skip] operator confirmation did not match"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr missing %q in:\n%s", want, got)
		}
	}
}

func TestNukeRunEDryRun(t *testing.T) {
	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf(errUnexpectedUI, err)
	}
	cfg := testConfig()
	cfg.DryRun = true
	clients := &platformaws.Clients{AccountID: "123456789012", Region: cfg.Region}
	setTestDeps(t, cfg, clients, presenter)

	oldInspect := inspectBootstrapStateStoresForNukeFn
	oldBackup := backupBootstrapStateStoresForNukeFn
	t.Cleanup(func() {
		inspectBootstrapStateStoresForNukeFn = oldInspect
		backupBootstrapStateStoresForNukeFn = oldBackup
	})
	inspectBootstrapStateStoresForNukeFn = func(context.Context, *config.Config, *platformaws.Clients) (bootstrapStateBackupPlan, error) {
		return bootstrapStateBackupPlan{}, nil
	}
	backupBootstrapStateStoresForNukeFn = func(context.Context, *config.Config, *platformaws.Clients, string, bootstrapStateBackupPlan) error {
		t.Fatal("backup should not run during dry-run")
		return nil
	}

	cmd, stdout, _ := newTestCommand(testCommandContext(presenter))
	if err := nukeCmd.RunE(cmd, nil); err != nil {
		t.Fatalf(errUnexpectedRunE, err)
	}
	if !strings.Contains(stdout.String(), "[ok] bootstrap resources removed") {
		t.Fatalf(errStdoutMissingOK, stdout.String())
	}
}

func TestNukeRunEBacksUpBootstrapStateBeforeDelete(t *testing.T) {
	cfg := testConfig()
	cfg.DryRun = false
	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf(errUnexpectedUI, err)
	}
	setTestDeps(t, cfg, &platformaws.Clients{}, presenter)

	oldInspect := inspectBootstrapStateStoresForNukeFn
	oldBackup := backupBootstrapStateStoresForNukeFn
	oldDefaultDir := defaultBootstrapBackupDirForNukeFn
	oldBootstrapNukeFn := bootstrapNukeFn
	t.Cleanup(func() {
		inspectBootstrapStateStoresForNukeFn = oldInspect
		backupBootstrapStateStoresForNukeFn = oldBackup
		defaultBootstrapBackupDirForNukeFn = oldDefaultDir
		bootstrapNukeFn = oldBootstrapNukeFn
	})
	inspectBootstrapStateStoresForNukeFn = func(context.Context, *config.Config, *platformaws.Clients) (bootstrapStateBackupPlan, error) {
		return bootstrapStateBackupPlan{
			StateBucket:        "acme-tf-state-root",
			StateBucketObjects: 2,
			LockTable:          "acme-tf-locks-root",
			LockTableItems:     1,
			RegistryTable:      "acme-bootstrap-registry",
			RegistryTableItems: 3,
		}, nil
	}
	var gotBackupDir string
	backupBootstrapStateStoresForNukeFn = func(_ context.Context, _ *config.Config, _ *platformaws.Clients, dir string, _ bootstrapStateBackupPlan) error {
		gotBackupDir = dir
		return nil
	}
	defaultBootstrapBackupDirForNukeFn = func(string) string {
		return filepath.Join(t.TempDir(), "backup")
	}
	bootstrapCalls := 0
	bootstrapNukeFn = func(context.Context, *config.Config, *platformaws.Clients, io.Writer) error {
		bootstrapCalls++
		return nil
	}

	oldStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = oldStdin })
	inputFile, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf(errCreateTemp, err)
	}
	if _, err := inputFile.WriteString("backup-nuke-acme\n"); err != nil {
		t.Fatalf(errWriteString, err)
	}
	if _, err := inputFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf(errSeek, err)
	}
	os.Stdin = inputFile

	cmd, stdout, stderr := newTestCommand(testCommandContext(presenter))
	if err := nukeCmd.RunE(cmd, nil); err != nil {
		t.Fatalf(errUnexpectedRunE, err)
	}
	if gotBackupDir == "" {
		t.Fatal("expected backupBootstrapStateStoresForNukeFn to be called")
	}
	if bootstrapCalls != 1 {
		t.Fatalf(errNukeCallCount, bootstrapCalls)
	}
	if !strings.Contains(stderr.String(), `Type "backup-nuke-acme" to confirm:`) {
		t.Fatalf("stderr missing backup confirmation in:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "[ok] bootstrap resources removed") {
		t.Fatalf(errStdoutMissingOK, stdout.String())
	}
}

func TestNukeRunEAllCancelledByConfirmation(t *testing.T) {
	cfg := testConfig()
	cfg.DryRun = false
	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf(errUnexpectedUI, err)
	}
	setTestDeps(t, cfg, &platformaws.Clients{}, presenter)

	oldNukeAll := nukeAll
	oldNukeEnv := nukeEnv
	oldRepoRootFn := nukeRepoRootFn
	oldRunStepFn := nukeRunStepFn
	oldBootstrapNukeFn := bootstrapNukeFn
	oldPreflightFn := nukePreflightAllFn
	t.Cleanup(func() {
		nukeAll = oldNukeAll
		nukeEnv = oldNukeEnv
		nukeRepoRootFn = oldRepoRootFn
		nukeRunStepFn = oldRunStepFn
		bootstrapNukeFn = oldBootstrapNukeFn
		nukePreflightAllFn = oldPreflightFn
	})
	nukeAll = true
	nukeEnv = "prod"
	nukePreflightAllFn = func(string, string) error { return nil }
	nukeRepoRootFn = func() (string, error) {
		return testBootstrapRepoRoot, nil
	}
	nukeRunStepFn = func(context.Context, bootstrapNukeAllStep, io.Writer, io.Writer) error {
		t.Fatal("runNukeAllStep should not be called when confirmation is rejected")
		return nil
	}
	bootstrapNukeFn = func(context.Context, *config.Config, *platformaws.Clients, io.Writer) error {
		t.Fatal("bootstrap.Nuke should not be called when confirmation is rejected")
		return nil
	}

	oldStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = oldStdin })
	inputFile, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf(errCreateTemp, err)
	}
	if _, err := inputFile.WriteString("nope\n"); err != nil {
		t.Fatalf(errWriteString, err)
	}
	if _, err := inputFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf(errSeek, err)
	}
	os.Stdin = inputFile

	cmd, _, stderr := newTestCommand(context.Background())
	if err := nukeCmd.RunE(cmd, nil); err != nil {
		t.Fatalf(errUnexpectedRunE, err)
	}
	got := stderr.String()
	for _, want := range []string{"Platform Bootstrap Nuke", "Steps to be executed:", "platform-org purge", "[skip] operator confirmation did not match"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr missing %q in:\n%s", want, got)
		}
	}
}

func TestNukeRunEAllDryRun(t *testing.T) {
	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf(errUnexpectedUI, err)
	}
	cfg := testConfig()
	cfg.DryRun = true
	clients := &platformaws.Clients{AccountID: "123456789012", Region: cfg.Region}
	setTestDeps(t, cfg, clients, presenter)

	oldNukeAll := nukeAll
	oldNukeEnv := nukeEnv
	oldRepoRootFn := nukeRepoRootFn
	oldRunStepFn := nukeRunStepFn
	oldBootstrapNukeFn := bootstrapNukeFn
	oldPreflightFn := nukePreflightAllFn
	t.Cleanup(func() {
		nukeAll = oldNukeAll
		nukeEnv = oldNukeEnv
		nukeRepoRootFn = oldRepoRootFn
		nukeRunStepFn = oldRunStepFn
		bootstrapNukeFn = oldBootstrapNukeFn
		nukePreflightAllFn = oldPreflightFn
	})
	nukeAll = true
	nukeEnv = "prod"
	nukePreflightAllFn = func(string, string) error { return nil }
	nukeRepoRootFn = func() (string, error) {
		return testBootstrapRepoRoot, nil
	}
	nukeRunStepFn = func(context.Context, bootstrapNukeAllStep, io.Writer, io.Writer) error {
		t.Fatal("runNukeAllStep should not be called during dry-run")
		return nil
	}
	bootstrapCalls := 0
	bootstrapNukeFn = func(context.Context, *config.Config, *platformaws.Clients, io.Writer) error {
		bootstrapCalls++
		return nil
	}

	cmd, stdout, _ := newTestCommand(testCommandContext(presenter))
	if err := nukeCmd.RunE(cmd, nil); err != nil {
		t.Fatalf(errUnexpectedRunE, err)
	}
	if bootstrapCalls != 1 {
		t.Fatalf(errNukeCallCount, bootstrapCalls)
	}
	got := stdout.String()
	for _, want := range []string{
		"[dry-run] Atlantis would run",
		"[dry-run] project-template would run",
		"[dry-run] github-oidc would run",
		"[dry-run] platform-org build would run",
		"[dry-run] platform-org purge would run",
		"[dry-run] platform-org nuke would run",
		"[ok] all platform resources removed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout missing %q in:\n%s", want, got)
		}
	}
}

func TestNukeRunEAllSuccess(t *testing.T) {
	cfg := testConfig()
	cfg.DryRun = false
	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf(errUnexpectedUI, err)
	}
	setTestDeps(t, cfg, &platformaws.Clients{}, presenter)

	oldNukeAll := nukeAll
	oldNukeEnv := nukeEnv
	oldRepoRootFn := nukeRepoRootFn
	oldRunStepFn := nukeRunStepFn
	oldBootstrapNukeFn := bootstrapNukeFn
	oldPreflightFn := nukePreflightAllFn
	t.Cleanup(func() {
		nukeAll = oldNukeAll
		nukeEnv = oldNukeEnv
		nukeRepoRootFn = oldRepoRootFn
		nukeRunStepFn = oldRunStepFn
		bootstrapNukeFn = oldBootstrapNukeFn
		nukePreflightAllFn = oldPreflightFn
	})
	nukeAll = true
	nukeEnv = "prod"
	nukePreflightAllFn = func(string, string) error { return nil }
	nukeRepoRootFn = func() (string, error) {
		return testBootstrapRepoRoot, nil
	}
	var gotSteps []string
	nukeRunStepFn = func(_ context.Context, step bootstrapNukeAllStep, _, _ io.Writer) error {
		gotSteps = append(gotSteps, step.label)
		return nil
	}
	bootstrapCalls := 0
	bootstrapNukeFn = func(context.Context, *config.Config, *platformaws.Clients, io.Writer) error {
		bootstrapCalls++
		return nil
	}

	oldStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = oldStdin })
	inputFile, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf(errCreateTemp, err)
	}
	if _, err := inputFile.WriteString("nuke-all-acme\n"); err != nil {
		t.Fatalf(errWriteString, err)
	}
	if _, err := inputFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf(errSeek, err)
	}
	os.Stdin = inputFile

	cmd, stdout, stderr := newTestCommand(context.Background())
	if err := nukeCmd.RunE(cmd, nil); err != nil {
		t.Fatalf(errUnexpectedRunE, err)
	}
	wantSteps := []string{
		"Atlantis",
		"project-template",
		"github-oidc",
		"platform-org build",
		"platform-org purge",
		"platform-org nuke",
	}
	if strings.Join(gotSteps, "|") != strings.Join(wantSteps, "|") {
		t.Fatalf("steps = %v; want %v", gotSteps, wantSteps)
	}
	if bootstrapCalls != 1 {
		t.Fatalf(errNukeCallCount, bootstrapCalls)
	}
	if !strings.Contains(stderr.String(), "Type \"nuke-all-acme\" to confirm:") {
		t.Fatalf("stderr missing confirmation prompt in:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "[ok] all platform resources removed") {
		t.Fatalf(errStdoutMissingOK, stdout.String())
	}
}

func TestNukeRunEAllPreflightFailsBeforeConfirmationAndBackup(t *testing.T) {
	cfg := testConfig()
	cfg.DryRun = false
	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf(errUnexpectedUI, err)
	}
	setTestDeps(t, cfg, &platformaws.Clients{}, presenter)

	oldNukeAll := nukeAll
	oldNukeEnv := nukeEnv
	oldRepoRootFn := nukeRepoRootFn
	oldRunStepFn := nukeRunStepFn
	oldBootstrapNukeFn := bootstrapNukeFn
	oldPreflightFn := nukePreflightAllFn
	oldInspect := inspectBootstrapStateStoresForNukeFn
	oldBackup := backupBootstrapStateStoresForNukeFn
	t.Cleanup(func() {
		nukeAll = oldNukeAll
		nukeEnv = oldNukeEnv
		nukeRepoRootFn = oldRepoRootFn
		nukeRunStepFn = oldRunStepFn
		bootstrapNukeFn = oldBootstrapNukeFn
		nukePreflightAllFn = oldPreflightFn
		inspectBootstrapStateStoresForNukeFn = oldInspect
		backupBootstrapStateStoresForNukeFn = oldBackup
	})
	nukeAll = true
	nukeEnv = "prod"
	nukeRepoRootFn = func() (string, error) {
		return testBootstrapRepoRoot, nil
	}
	nukePreflightAllFn = func(string, string) error {
		return errors.New("Atlantis preflight failed: envs/prod/backend.hcl is missing required backend keys: bucket, region")
	}
	nukeRunStepFn = func(context.Context, bootstrapNukeAllStep, io.Writer, io.Writer) error {
		t.Fatal("runNukeAllStep should not be called when preflight fails")
		return nil
	}
	bootstrapNukeFn = func(context.Context, *config.Config, *platformaws.Clients, io.Writer) error {
		t.Fatal("bootstrapNukeFn should not be called when preflight fails")
		return nil
	}
	inspectBootstrapStateStoresForNukeFn = func(context.Context, *config.Config, *platformaws.Clients) (bootstrapStateBackupPlan, error) {
		return bootstrapStateBackupPlan{
			StateBucket:        "acme-tf-state-root",
			StateBucketObjects: 1,
		}, nil
	}
	backupBootstrapStateStoresForNukeFn = func(context.Context, *config.Config, *platformaws.Clients, string, bootstrapStateBackupPlan) error {
		t.Fatal("backup should not run when preflight fails")
		return nil
	}

	cmd, _, _ := newTestCommand(context.Background())
	err = nukeCmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected nuke --all preflight to fail")
	}
	if !strings.Contains(err.Error(), "Atlantis preflight failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchRunESuccess(t *testing.T) {
	cfg := testConfig()
	accountRec := platformaws.ConfigRecord{
		PK:         "CONFIG#account",
		SK:         "dev",
		ConfigType: "account",
		ConfigName: "dev",
		Data:       map[string]string{"email": "dev@example.com"},
	}
	adminRec := platformaws.ConfigRecord{
		PK:         "CONFIG#admin",
		SK:         "alert_email",
		ConfigType: "admin",
		ConfigName: "alert_email",
		Data:       map[string]string{"email": "alerts@example.com"},
	}
	accountItem, err := attributevalue.MarshalMap(accountRec)
	if err != nil {
		t.Fatalf("MarshalMap(account) unexpected error: %v", err)
	}
	adminItem, err := attributevalue.MarshalMap(adminRec)
	if err != nil {
		t.Fatalf("MarshalMap(admin) unexpected error: %v", err)
	}

	clients := &platformaws.Clients{
		DynamoDB: &cmdDynamoDBMock{
			scanFn: func(in *dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
				pk := in.ExpressionAttributeValues[":pk"].(*dbtypes.AttributeValueMemberS).Value
				switch pk {
				case "CONFIG#account":
					return &dynamodb.ScanOutput{Items: []map[string]dbtypes.AttributeValue{accountItem}}, nil
				case "CONFIG#admin":
					return &dynamodb.ScanOutput{Items: []map[string]dbtypes.AttributeValue{adminItem}}, nil
				default:
					return &dynamodb.ScanOutput{}, nil
				}
			},
		},
	}
	setTestDeps(t, cfg, clients, nil)

	cmd, _, _ := newTestCommand(context.Background())
	cmd.Flags().String("output", "-", "")
	cmd.Flags().String(flagBackendOut, "", "")
	backendPath := filepath.Join(t.TempDir(), "stack", "backend.local.hcl")
	if err := cmd.Flags().Set(flagBackendOut, backendPath); err != nil {
		t.Fatalf(errFlagsSet, err)
	}

	stdout := captureStdout(t, func() {
		if err := fetchCmd.RunE(cmd, nil); err != nil {
			t.Fatalf(errUnexpectedRunE, err)
		}
	})
	if !strings.Contains(stdout, `"budget_alert_email": "alerts@example.com"`) {
		t.Fatalf("stdout missing fetched config:\n%s", stdout)
	}
	data, err := os.ReadFile(backendPath)
	if err != nil {
		t.Fatalf("ReadFile() unexpected error: %v", err)
	}
	if !strings.Contains(string(data), `bucket         = "acme-tf-state-root"`) {
		t.Fatalf("backend file missing bucket:\n%s", string(data))
	}
}

func TestFetchRunEError(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		DynamoDB: &cmdDynamoDBMock{
			scanFn: func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
				return nil, errors.New("dynamo down")
			},
		},
	}
	setTestDeps(t, cfg, clients, nil)

	cmd, _, _ := newTestCommand(context.Background())
	cmd.Flags().String("output", "-", "")
	cmd.Flags().String(flagBackendOut, "", "")

	err := fetchCmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected fetch to fail")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitAWSError {
		t.Fatalf(errUnexpected, err)
	}
	if !strings.Contains(err.Error(), "fetching account config") {
		t.Fatalf(errUnexpectedText, err)
	}
}

func TestRootPersistentPreRunEInvalidUI(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	f := cmd.Flags()
	f.String("org", "", "")
	f.String("profile", "", "")
	f.String("region", "", "")
	f.String("log-level", "", "")
	f.Bool("dry-run", false, "")
	f.String("ui", "auto", "")
	if err := f.Set("org", "acme"); err != nil {
		t.Fatalf("Set(org) unexpected error: %v", err)
	}
	if err := f.Set("region", "us-east-1"); err != nil {
		t.Fatalf("Set(region) unexpected error: %v", err)
	}
	if err := f.Set("ui", "nope"); err != nil {
		t.Fatalf("Set(ui) unexpected error: %v", err)
	}

	err := rootCmd.PersistentPreRunE(cmd, nil)
	if err == nil {
		t.Fatal("expected PersistentPreRunE to fail")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitUserError {
		t.Fatalf(errUnexpected, err)
	}
	if !strings.Contains(err.Error(), "invalid ui mode") {
		t.Fatalf(errUnexpectedText, err)
	}
}

func TestRootPersistentPreRunENoCredentials(t *testing.T) {
	t.Setenv("PLATFORM_ORG_NAME", "")
	t.Setenv("PLATFORM_AWS_PROFILE", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	f := cmd.Flags()
	f.String("org", "", "")
	f.String("profile", "", "")
	f.String("region", "", "")
	f.String("log-level", "", "")
	f.Bool("dry-run", false, "")
	f.String("ui", "auto", "")
	if err := f.Set("org", "acme"); err != nil {
		t.Fatalf("Set(org) unexpected error: %v", err)
	}
	if err := f.Set("region", "us-east-1"); err != nil {
		t.Fatalf("Set(region) unexpected error: %v", err)
	}
	if err := f.Set("ui", "plain"); err != nil {
		t.Fatalf("Set(ui) unexpected error: %v", err)
	}

	err := rootCmd.PersistentPreRunE(cmd, nil)
	if err == nil {
		t.Fatal("expected PersistentPreRunE to fail")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitUserError {
		t.Fatalf(errUnexpected, err)
	}
	if !strings.Contains(err.Error(), "no AWS credentials configured") {
		t.Fatalf(errUnexpectedText, err)
	}
}

func TestExitErrorMethods(t *testing.T) {
	base := errors.New("boom")
	err := &ExitError{Code: 7, Err: base}
	if got := err.Error(); got != "boom" {
		t.Fatalf("Error() = %q", got)
	}
	if !errors.Is(err, base) {
		t.Fatal("Unwrap() did not expose base error")
	}
}
