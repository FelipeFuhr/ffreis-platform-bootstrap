package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
)

func TestBootstrapRegistrySectionDuplicateConflict(t *testing.T) {
	cfg := testConfig()
	first, err := platformaws.NewRegistryRecord("S3Bucket", cfg.StateBucketName(), "creator-a", map[string]string{"ManagedBy": "bootstrap"})
	if err != nil {
		t.Fatalf("NewRegistryRecord() unexpected error: %v", err)
	}
	second, err := platformaws.NewRegistryRecord("S3Bucket", cfg.StateBucketName(), "creator-b", map[string]string{"ManagedBy": "bootstrap"})
	if err != nil {
		t.Fatalf("NewRegistryRecord() unexpected error: %v", err)
	}
	first.CreatedAt = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	second.CreatedAt = first.CreatedAt.Add(time.Minute)
	firstItem, err := attributevalue.MarshalMap(first)
	if err != nil {
		t.Fatalf("MarshalMap(first) unexpected error: %v", err)
	}
	secondItem, err := attributevalue.MarshalMap(second)
	if err != nil {
		t.Fatalf("MarshalMap(second) unexpected error: %v", err)
	}

	clients := &platformaws.Clients{
		DynamoDB: &cmdDynamoDBMock{
			scanFn: func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
				return &dynamodb.ScanOutput{Items: []map[string]dbtypes.AttributeValue{firstItem, secondItem}}, nil
			},
			describeTableFn: func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
				return nil, &dbtypes.ResourceNotFoundException{}
			},
		},
		S3:      &cmdS3Mock{},
		IAM:     &cmdIAMMock{},
		SNS:     &cmdSNSMock{},
		Budgets: &cmdBudgetsMock{},
	}
	setTestDeps(t, cfg, clients, nil)

	section, err := bootstrapRegistrySection(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if section.Checks[0].Status != "fail" {
		t.Fatalf("duplicates check status = %q, want fail", section.Checks[0].Status)
	}
	if !strings.Contains(section.Checks[0].Detail, "conflicting duplicate registry rows") {
		t.Fatalf("unexpected duplicate detail: %s", section.Checks[0].Detail)
	}
}

func TestBootstrapContractSectionWithoutTrust(t *testing.T) {
	setTestDeps(t, testConfig(), &platformaws.Clients{}, nil)

	section, err := bootstrapContractSection(context.Background(), bootstrapDoctorMode{IncludeContract: true, IncludeTrust: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(section.Checks) != 2 {
		t.Fatalf("expected 2 contract checks without trust, got %d", len(section.Checks))
	}
}

func TestBootstrapTrustCheckDecodeAndJSONFailures(t *testing.T) {
	t.Run("decode failure", func(t *testing.T) {
		clients := &platformaws.Clients{
			AccountID: "123456789012",
			IAM: &cmdIAMMock{
				getRoleFn: func(*iam.GetRoleInput) (*iam.GetRoleOutput, error) {
					return &iam.GetRoleOutput{Role: &iamtypes.Role{AssumeRolePolicyDocument: aws.String("%zz")}}, nil
				},
			},
		}
		setTestDeps(t, testConfig(), clients, nil)

		check := bootstrapTrustCheck(context.Background(), true)
		if check.Status != "fail" || !strings.Contains(check.Detail, "could not decode trust policy") {
			t.Fatalf("unexpected decode failure check: %+v", check)
		}
	})

	t.Run("json failure", func(t *testing.T) {
		clients := &platformaws.Clients{
			AccountID: "123456789012",
			IAM: &cmdIAMMock{
				getRoleFn: func(*iam.GetRoleInput) (*iam.GetRoleOutput, error) {
					return &iam.GetRoleOutput{Role: &iamtypes.Role{AssumeRolePolicyDocument: aws.String("not-json")}}, nil
				},
			},
		}
		setTestDeps(t, testConfig(), clients, nil)

		check := bootstrapTrustCheck(context.Background(), true)
		if check.Status != "fail" || !strings.Contains(check.Detail, "could not parse trust policy JSON") {
			t.Fatalf("unexpected JSON failure check: %+v", check)
		}
	})
}

func TestBootstrapDoctorStatusIconVariants(t *testing.T) {
	setTestDeps(t, testConfig(), &platformaws.Clients{}, nil)
	if got := bootstrapDoctorStatusIcon("warn"); got != "WARN" {
		t.Fatalf("bootstrapDoctorStatusIcon(warn) = %q", got)
	}
	if got := bootstrapDoctorStatusIcon("info"); got != "INFO" {
		t.Fatalf("bootstrapDoctorStatusIcon(info) = %q", got)
	}
	if got := bootstrapDoctorStatusIcon("mystery"); got != "mystery" {
		t.Fatalf("bootstrapDoctorStatusIcon(mystery) = %q", got)
	}

	if got := strconvQuote("acme"); got != string(mustMarshalJSON(t, "acme")) {
		t.Fatalf("strconvQuote() = %q", got)
	}
}

func mustMarshalJSON(t *testing.T, value string) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() unexpected error: %v", err)
	}
	return data
}
