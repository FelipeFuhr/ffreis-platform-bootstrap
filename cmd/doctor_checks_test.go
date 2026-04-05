package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	budgetstypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
)

// --- summarizeBootstrapDoctor ---

func TestSummarizeBootstrapDoctor(t *testing.T) {
	sections := []bootstrapDoctorSection{
		{Checks: []bootstrapDoctorCheck{
			{Status: "ok"},
			{Status: "ok"},
			{Status: "warn"},
			{Status: "fail"},
			{Status: "info"},
		}},
	}
	got := summarizeBootstrapDoctor(sections)
	if got.OK != 2 || got.Warn != 1 || got.Fail != 1 || got.Info != 1 || got.Total != 5 {
		t.Errorf("unexpected summary: %+v", got)
	}
}

func TestBootstrapDoctorReportHasFailures(t *testing.T) {
	noFail := BootstrapDoctorReport{Summary: bootstrapDoctorSummary{OK: 1}}
	if noFail.HasFailures() {
		t.Error("expected no failures")
	}
	withFail := BootstrapDoctorReport{Summary: bootstrapDoctorSummary{Fail: 1}}
	if !withFail.HasFailures() {
		t.Error("expected failures")
	}
}

// --- bootstrapPermissionSection ---

func TestBootstrapPermissionSection_AllOK(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		IAM:       &cmdIAMMock{},
		S3:        &cmdS3Mock{},
		DynamoDB:  &cmdDynamoDBMock{},
		SNS:       &cmdSNSMock{},
		Budgets:   &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	section := bootstrapPermissionSection(context.Background())
	for _, check := range section.Checks {
		if check.Status != "ok" {
			t.Errorf("check %q: status = %q, want ok; detail: %s", check.Key, check.Status, check.Detail)
		}
	}
}

func TestBootstrapPermissionSection_IAMFailure(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		IAM:       &cmdIAMMock{getAccountSummaryErr: errors.New("access denied")},
		S3:        &cmdS3Mock{},
		DynamoDB:  &cmdDynamoDBMock{},
		SNS:       &cmdSNSMock{},
		Budgets:   &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	section := bootstrapPermissionSection(context.Background())
	var iamCheck *bootstrapDoctorCheck
	for i, c := range section.Checks {
		if c.Key == "iam:get-account-summary" {
			iamCheck = &section.Checks[i]
			break
		}
	}
	if iamCheck == nil {
		t.Fatal("iam check not found")
	}
	if iamCheck.Status != "fail" {
		t.Errorf("expected fail, got %q", iamCheck.Status)
	}
	if !iamCheck.Blocking {
		t.Error("failing permission check should be blocking")
	}
}

// --- bootstrapResourceSection ---

func TestBootstrapResourceSection_AllPresent(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		DynamoDB: &cmdDynamoDBMock{
			describeTableFn: func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
				return &dynamodb.DescribeTableOutput{Table: &dbtypes.TableDescription{TableStatus: dbtypes.TableStatusActive}}, nil
			},
		},
		S3: &cmdS3Mock{
			headBucketFn: func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				return &s3.HeadBucketOutput{}, nil
			},
		},
		IAM: &cmdIAMMock{
			getRoleFn: func(*iam.GetRoleInput) (*iam.GetRoleOutput, error) {
				return &iam.GetRoleOutput{Role: &iamtypes.Role{RoleName: aws.String("platform-admin")}}, nil
			},
		},
		SNS: &cmdSNSMock{
			getTopicAttributesFn: func(*sns.GetTopicAttributesInput) (*sns.GetTopicAttributesOutput, error) {
				return &sns.GetTopicAttributesOutput{Attributes: map[string]string{"TopicArn": "arn:aws:sns:us-east-1:123456789012:acme-platform-events"}}, nil
			},
		},
		Budgets: &cmdBudgetsMock{
			describeBudgetFn: func(*budgets.DescribeBudgetInput) (*budgets.DescribeBudgetOutput, error) {
				return &budgets.DescribeBudgetOutput{Budget: &budgetstypes.Budget{BudgetName: aws.String(cfg.BudgetName())}}, nil
			},
		},
	}
	setTestDeps(t, cfg, clients, nil)

	section := bootstrapResourceSection(context.Background(), true)
	for _, check := range section.Checks {
		if check.Status != "ok" {
			t.Errorf("check %q: status = %q, want ok; detail: %s", check.Key, check.Status, check.Detail)
		}
	}
}

func TestBootstrapResourceSection_MissingRequiredFails(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		DynamoDB: &cmdDynamoDBMock{
			describeTableFn: func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
				return nil, &dbtypes.ResourceNotFoundException{}
			},
		},
		S3: &cmdS3Mock{
			headBucketFn: func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				return nil, &s3types.NotFound{}
			},
		},
		IAM:     &cmdIAMMock{},
		SNS:     &cmdSNSMock{},
		Budgets: &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	section := bootstrapResourceSection(context.Background(), true)
	fails := 0
	for _, check := range section.Checks {
		if check.Status == "fail" {
			fails++
		}
	}
	if fails == 0 {
		t.Error("expected at least one failing check for missing resources")
	}
}

func TestBootstrapResourceSection_MissingNotRequired_IsInfo(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		DynamoDB: &cmdDynamoDBMock{
			describeTableFn: func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
				return nil, &dbtypes.ResourceNotFoundException{}
			},
		},
		S3: &cmdS3Mock{
			headBucketFn: func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				return nil, &s3types.NotFound{}
			},
		},
		IAM:     &cmdIAMMock{},
		SNS:     &cmdSNSMock{},
		Budgets: &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	section := bootstrapResourceSection(context.Background(), false)
	for _, check := range section.Checks {
		if check.Status == "fail" {
			t.Errorf("check %q should be info (not required), got fail", check.Key)
		}
	}
}

// --- bootstrapRegistrySection ---

func TestBootstrapRegistrySection_NoDuplicates(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		DynamoDB: &cmdDynamoDBMock{
			scanFn: func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
				return &dynamodb.ScanOutput{}, nil
			},
			describeTableFn: func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
				return nil, &dbtypes.ResourceNotFoundException{}
			},
		},
		S3: &cmdS3Mock{
			headBucketFn: func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
				return nil, &s3types.NotFound{}
			},
		},
		IAM:     &cmdIAMMock{},
		SNS:     &cmdSNSMock{},
		Budgets: &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	section, err := bootstrapRegistrySection(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var dupCheck *bootstrapDoctorCheck
	for i, c := range section.Checks {
		if c.Key == "registry.duplicates" {
			dupCheck = &section.Checks[i]
			break
		}
	}
	if dupCheck == nil {
		t.Fatal("duplicates check not found")
	}
	if dupCheck.Status != "ok" {
		t.Errorf("expected ok for no duplicates, got %q", dupCheck.Status)
	}
}

func TestBootstrapRegistrySection_ScanError(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		DynamoDB: &cmdDynamoDBMock{
			scanFn: func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
				return nil, errors.New("scan error")
			},
		},
		S3:      &cmdS3Mock{},
		IAM:     &cmdIAMMock{},
		SNS:     &cmdSNSMock{},
		Budgets: &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	_, err := bootstrapRegistrySection(context.Background(), true)
	if err == nil {
		t.Error("expected error from scan failure")
	}
}

// --- bootstrapTrustCheck ---

func TestBootstrapTrustCheck_RoleMissingNotRequired(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		IAM:       &cmdIAMMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	check := bootstrapTrustCheck(context.Background(), false)
	if check.Status != "info" {
		t.Errorf("expected info for missing role (not required), got %q: %s", check.Status, check.Detail)
	}
}

func TestBootstrapTrustCheck_RoleMissingRequired(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		IAM:       &cmdIAMMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	check := bootstrapTrustCheck(context.Background(), true)
	if check.Status != "fail" {
		t.Errorf("expected fail for missing role (required), got %q", check.Status)
	}
	if !check.Blocking {
		t.Error("missing required role check should be blocking")
	}
}

func TestBootstrapTrustCheck_TrustContainsAccountRoot(t *testing.T) {
	cfg := testConfig()
	accountID := "123456789012"
	trustDoc := url.QueryEscape(fmt.Sprintf(`{"Statement":[{"Principal":{"AWS":"arn:aws:iam::%s:root"}}]}`, accountID))
	clients := &platformaws.Clients{
		AccountID: accountID,
		IAM: &cmdIAMMock{
			getRoleFn: func(*iam.GetRoleInput) (*iam.GetRoleOutput, error) {
				return &iam.GetRoleOutput{
					Role: &iamtypes.Role{
						RoleName:                 aws.String("platform-admin"),
						AssumeRolePolicyDocument: aws.String(trustDoc),
					},
				}, nil
			},
		},
	}
	setTestDeps(t, cfg, clients, nil)

	check := bootstrapTrustCheck(context.Background(), true)
	if check.Status != "ok" {
		t.Errorf("expected ok for valid trust policy, got %q: %s", check.Status, check.Detail)
	}
}

func TestBootstrapTrustCheck_TrustMissingAccountRoot(t *testing.T) {
	cfg := testConfig()
	accountID := "123456789012"
	trustDoc := url.QueryEscape(`{"Statement":[{"Principal":{"AWS":"arn:aws:iam::999999999999:root"}}]}`)
	clients := &platformaws.Clients{
		AccountID: accountID,
		IAM: &cmdIAMMock{
			getRoleFn: func(*iam.GetRoleInput) (*iam.GetRoleOutput, error) {
				return &iam.GetRoleOutput{
					Role: &iamtypes.Role{
						RoleName:                 aws.String("platform-admin"),
						AssumeRolePolicyDocument: aws.String(trustDoc),
					},
				}, nil
			},
		},
	}
	setTestDeps(t, cfg, clients, nil)

	check := bootstrapTrustCheck(context.Background(), true)
	if check.Status != "fail" {
		t.Errorf("expected fail for trust without account root, got %q", check.Status)
	}
	if !strings.Contains(check.Detail, "does not include") {
		t.Errorf("detail should explain missing principal, got: %s", check.Detail)
	}
}

// --- bootstrapContractSection ---

func TestBootstrapContractSection_IncludesContractAndTrustChecks(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		IAM:       &cmdIAMMock{},
		S3:        &cmdS3Mock{},
		DynamoDB:  &cmdDynamoDBMock{},
		SNS:       &cmdSNSMock{},
		Budgets:   &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	mode := bootstrapDoctorModes.command
	section, err := bootstrapContractSection(context.Background(), mode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if section.Title != "Exported Contract" {
		t.Errorf("unexpected section title: %q", section.Title)
	}
	keys := make(map[string]bool)
	for _, c := range section.Checks {
		keys[c.Key] = true
	}
	for _, want := range []string{"contract.backend-export", "contract.backend-render", "contract.platform-admin-trust"} {
		if !keys[want] {
			t.Errorf("check %q not found in section", want)
		}
	}
}

// --- bootstrapDoctorStatusIcon ---

func TestBootstrapDoctorStatusIcon(t *testing.T) {
	cfg := testConfig()
	setTestDeps(t, cfg, &platformaws.Clients{}, nil)

	cases := map[string]string{
		"ok":      "OK  ",
		"warn":    "WARN",
		"fail":    "FAIL",
		"info":    "INFO",
		"unknown": "unknown",
	}
	for status, want := range cases {
		got := bootstrapDoctorStatusIcon(status)
		if got != want {
			t.Errorf("bootstrapDoctorStatusIcon(%q) = %q, want %q", status, got, want)
		}
	}
}

// --- runBootstrapDoctor (mode integration) ---

func TestRunBootstrapDoctor_InitMode_SkipsResourcesAndRegistry(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		IAM:       &cmdIAMMock{},
		S3:        &cmdS3Mock{},
		DynamoDB:  &cmdDynamoDBMock{},
		SNS:       &cmdSNSMock{},
		Budgets:   &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	report, err := runBootstrapDoctor(context.Background(), bootstrapDoctorModes.init)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	titles := make(map[string]bool)
	for _, s := range report.Sections {
		titles[s.Title] = true
	}
	if titles["Layer 0 Resources"] {
		t.Error("init mode should not include Layer 0 Resources section")
	}
	if titles["Registry Integrity"] {
		t.Error("init mode should not include Registry Integrity section")
	}
	if !titles["Permissions"] {
		t.Error("init mode should include Permissions section")
	}
}

func TestRunBootstrapDoctor_AuditMode_SkipsPermissions(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		IAM:       &cmdIAMMock{},
		S3:        &cmdS3Mock{},
		DynamoDB:  &cmdDynamoDBMock{},
		SNS:       &cmdSNSMock{},
		Budgets:   &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	report, err := runBootstrapDoctor(context.Background(), bootstrapDoctorModes.audit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, s := range report.Sections {
		if s.Title == "Permissions" {
			t.Error("audit mode should not include Permissions section")
		}
	}
}

// --- SNS and Budgets permission checks ---

func TestBootstrapPermissionSection_SNSFailure(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		IAM:       &cmdIAMMock{},
		S3:        &cmdS3Mock{},
		DynamoDB:  &cmdDynamoDBMock{},
		SNS:       &cmdSNSMock{listTopicsErr: errors.New("sns denied")},
		Budgets:   &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	section := bootstrapPermissionSection(context.Background())
	var snsCheck *bootstrapDoctorCheck
	for i, c := range section.Checks {
		if c.Key == "sns:list-topics" {
			snsCheck = &section.Checks[i]
		}
	}
	if snsCheck == nil {
		t.Fatal("sns check not found")
	}
	if snsCheck.Status != "fail" {
		t.Errorf("expected fail for sns error, got %q", snsCheck.Status)
	}
}

func TestBootstrapPermissionSection_BudgetsFailure(t *testing.T) {
	cfg := testConfig()
	clients := &platformaws.Clients{
		AccountID: "123456789012",
		IAM:       &cmdIAMMock{},
		S3:        &cmdS3Mock{},
		DynamoDB:  &cmdDynamoDBMock{},
		SNS:       &cmdSNSMock{},
		Budgets:   &cmdBudgetsMock{describeBudgetsErr: errors.New("budgets denied")},
	}
	setTestDeps(t, cfg, clients, nil)

	section := bootstrapPermissionSection(context.Background())
	var budgetsCheck *bootstrapDoctorCheck
	for i, c := range section.Checks {
		if c.Key == "budgets:describe-budgets" {
			budgetsCheck = &section.Checks[i]
		}
	}
	if budgetsCheck == nil {
		t.Fatal("budgets check not found")
	}
	if budgetsCheck.Status != "fail" {
		t.Errorf("expected fail for budgets error, got %q", budgetsCheck.Status)
	}
}

// --- registryResourceStatus ---

func TestRegistryResourceStatus(t *testing.T) {
	tests := []struct {
		name            string
		registered      bool
		exists          bool
		requireExisting bool
		wantStatus      string
	}{
		{"exists in AWS but not registered", false, true, false, "fail"},
		{"both missing, require existing", false, false, true, "fail"},
		{"both missing, not required", false, false, false, "info"},
		{"registered but gone, require existing", true, false, true, "fail"},
		{"registered but gone, not required", true, false, false, "ok"},
		{"registered and present", true, true, false, "ok"},
		{"registered and present, require existing", true, true, true, "ok"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, detail := registryResourceStatus(tc.registered, tc.exists, tc.requireExisting)
			if status != tc.wantStatus {
				t.Errorf("status = %q, want %q (detail: %s)", status, tc.wantStatus, detail)
			}
			if detail == "" {
				t.Error("detail should not be empty")
			}
		})
	}
}
