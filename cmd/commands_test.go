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
	platformui "github.com/ffreis/platform-bootstrap/internal/ui"
)

const (
	testBootstrapRoleARN = "arn:aws:iam::123456789012:role/bootstrap"
	errUnexpected        = "unexpected error: %v"
	errUnexpectedText    = "unexpected error text: %v"
	errUnexpectedUI      = "ui.New() unexpected error: %v"
	errUnexpectedRunE    = "RunE() unexpected error: %v"
	errOutputMissing     = "output missing %q in:\n%s"
	flagBackendOut       = "backend-out"
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
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("Flags().Set() unexpected error: %v", err)
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
		Resources: []AuditResult{{ResourceType: "S3Bucket", ResourceName: "bucket", Status: "ok"}},
		Summary:   AuditSummary{Total: 1, OK: 1},
	})

	got := stdout.String()
	for _, want := range []string{"Platform Bootstrap Audit", "STATUS", "Summary:"} {
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
	if err := doctorCmd.RunE(cmd, nil); err != nil {
		t.Fatalf(errUnexpectedRunE, err)
	}

	got := stdout.String()
	for _, want := range []string{"platform-bootstrap doctor", "Checks:", "All checks passed."} {
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
	err := doctorCmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected doctor to fail")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitAWSError {
		t.Fatalf(errUnexpected, err)
	}
	got := stdout.String()
	for _, want := range []string{"doctor failed: 1 check(s) failed", "aws sso login --profile bootstrap", "error: denied line2"} {
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
		t.Fatalf("CreateTemp() unexpected error: %v", err)
	}
	if _, err := inputFile.WriteString("nope\n"); err != nil {
		t.Fatalf("WriteString() unexpected error: %v", err)
	}
	if _, err := inputFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek() unexpected error: %v", err)
	}
	os.Stdin = inputFile

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

	cmd, stdout, _ := newTestCommand(testCommandContext(presenter))
	if err := nukeCmd.RunE(cmd, nil); err != nil {
		t.Fatalf(errUnexpectedRunE, err)
	}
	if !strings.Contains(stdout.String(), "[ok] bootstrap resources removed") {
		t.Fatalf("stdout missing success status in:\n%s", stdout.String())
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
		t.Fatalf("Flags().Set() unexpected error: %v", err)
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
