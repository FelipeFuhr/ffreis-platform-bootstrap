package cmd

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/budgets"
	budgetstypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/spf13/cobra"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
	"github.com/ffreis/platform-bootstrap/internal/logging"
	platformui "github.com/ffreis/platform-bootstrap/internal/ui"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig() *config.Config {
	return &config.Config{
		OrgName:          "acme",
		RootEmail:        "root@example.com",
		Region:           "us-east-1",
		StateRegion:      "us-east-1",
		LogLevel:         "info",
		DryRun:           false,
		BudgetMonthlyUSD: 25,
		Accounts:         map[string]string{},
		ToolVersion:      "test",
	}
}

func testCommandContext(presenter *platformui.Presenter) context.Context {
	ctx := logging.WithLogger(context.Background(), testLogger())
	if presenter != nil {
		ctx = platformui.WithPresenter(ctx, presenter)
	}
	return ctx
}

func setTestDeps(t *testing.T, cfg *config.Config, clients *platformaws.Clients, presenter *platformui.Presenter) {
	t.Helper()

	oldDeps := deps
	oldRootCtx := rootCmd.Context()
	t.Cleanup(func() {
		deps = oldDeps
		rootCmd.SetContext(oldRootCtx)
	})

	deps.cfg = cfg
	deps.logger = testLogger()
	deps.clients = clients
	deps.ui = presenter
	rootCmd.SetContext(testCommandContext(presenter))
}

func newTestCommand(ctx context.Context) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(ctx)
	return cmd, &stdout, &stderr
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() unexpected error: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	return <-done
}

type cmdDynamoDBMock struct {
	scanFn          func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error)
	describeTableFn func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error)
	listTablesErr   error
}

func (m *cmdDynamoDBMock) DescribeTable(_ context.Context, in *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	if m.describeTableFn != nil {
		return m.describeTableFn(in)
	}
	return &dynamodb.DescribeTableOutput{}, nil
}

func (m *cmdDynamoDBMock) ListTables(context.Context, *dynamodb.ListTablesInput, ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return &dynamodb.ListTablesOutput{}, m.listTablesErr
}

func (m *cmdDynamoDBMock) CreateTable(context.Context, *dynamodb.CreateTableInput, ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	return &dynamodb.CreateTableOutput{}, nil
}

func (m *cmdDynamoDBMock) TagResource(context.Context, *dynamodb.TagResourceInput, ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
	return &dynamodb.TagResourceOutput{}, nil
}

func (m *cmdDynamoDBMock) PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return &dynamodb.PutItemOutput{}, nil
}

func (m *cmdDynamoDBMock) Scan(_ context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	if m.scanFn != nil {
		return m.scanFn(in)
	}
	return &dynamodb.ScanOutput{}, nil
}

func (m *cmdDynamoDBMock) DeleteTable(context.Context, *dynamodb.DeleteTableInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error) {
	return &dynamodb.DeleteTableOutput{}, nil
}

type cmdIAMMock struct {
	getAccountSummaryErr error
	getRoleFn            func(*iam.GetRoleInput) (*iam.GetRoleOutput, error)
}

func (m *cmdIAMMock) GetRole(_ context.Context, in *iam.GetRoleInput, _ ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	if m.getRoleFn != nil {
		return m.getRoleFn(in)
	}
	return nil, &iamtypes.NoSuchEntityException{}
}

func (m *cmdIAMMock) GetAccountSummary(context.Context, *iam.GetAccountSummaryInput, ...func(*iam.Options)) (*iam.GetAccountSummaryOutput, error) {
	return &iam.GetAccountSummaryOutput{}, m.getAccountSummaryErr
}

func (m *cmdIAMMock) CreateRole(context.Context, *iam.CreateRoleInput, ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	return &iam.CreateRoleOutput{}, nil
}

func (m *cmdIAMMock) PutRolePolicy(context.Context, *iam.PutRolePolicyInput, ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	return &iam.PutRolePolicyOutput{}, nil
}

func (m *cmdIAMMock) TagRole(context.Context, *iam.TagRoleInput, ...func(*iam.Options)) (*iam.TagRoleOutput, error) {
	return &iam.TagRoleOutput{}, nil
}

func (m *cmdIAMMock) ListRolePolicies(context.Context, *iam.ListRolePoliciesInput, ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error) {
	return &iam.ListRolePoliciesOutput{}, nil
}

func (m *cmdIAMMock) DeleteRolePolicy(context.Context, *iam.DeleteRolePolicyInput, ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
	return &iam.DeleteRolePolicyOutput{}, nil
}

func (m *cmdIAMMock) DeleteRole(context.Context, *iam.DeleteRoleInput, ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
	return &iam.DeleteRoleOutput{}, nil
}

func (m *cmdIAMMock) CreateUser(context.Context, *iam.CreateUserInput, ...func(*iam.Options)) (*iam.CreateUserOutput, error) {
	return &iam.CreateUserOutput{}, nil
}

func (m *cmdIAMMock) PutUserPolicy(context.Context, *iam.PutUserPolicyInput, ...func(*iam.Options)) (*iam.PutUserPolicyOutput, error) {
	return &iam.PutUserPolicyOutput{}, nil
}

func (m *cmdIAMMock) CreateAccessKey(context.Context, *iam.CreateAccessKeyInput, ...func(*iam.Options)) (*iam.CreateAccessKeyOutput, error) {
	return &iam.CreateAccessKeyOutput{}, nil
}

func (m *cmdIAMMock) ListAccessKeys(context.Context, *iam.ListAccessKeysInput, ...func(*iam.Options)) (*iam.ListAccessKeysOutput, error) {
	return &iam.ListAccessKeysOutput{}, nil
}

func (m *cmdIAMMock) DeleteAccessKey(context.Context, *iam.DeleteAccessKeyInput, ...func(*iam.Options)) (*iam.DeleteAccessKeyOutput, error) {
	return &iam.DeleteAccessKeyOutput{}, nil
}

func (m *cmdIAMMock) DeleteUserPolicy(context.Context, *iam.DeleteUserPolicyInput, ...func(*iam.Options)) (*iam.DeleteUserPolicyOutput, error) {
	return &iam.DeleteUserPolicyOutput{}, nil
}

func (m *cmdIAMMock) DeleteUser(context.Context, *iam.DeleteUserInput, ...func(*iam.Options)) (*iam.DeleteUserOutput, error) {
	return &iam.DeleteUserOutput{}, nil
}

func (m *cmdIAMMock) GetUser(context.Context, *iam.GetUserInput, ...func(*iam.Options)) (*iam.GetUserOutput, error) {
	return nil, &iamtypes.NoSuchEntityException{}
}

type cmdS3Mock struct {
	headBucketFn   func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error)
	listBucketsErr error
}

func (m *cmdS3Mock) HeadBucket(_ context.Context, in *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.headBucketFn != nil {
		return m.headBucketFn(in)
	}
	return &s3.HeadBucketOutput{}, nil
}

func (m *cmdS3Mock) ListBuckets(context.Context, *s3.ListBucketsInput, ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	return &s3.ListBucketsOutput{}, m.listBucketsErr
}

func (m *cmdS3Mock) CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return &s3.CreateBucketOutput{}, nil
}

func (m *cmdS3Mock) PutBucketVersioning(context.Context, *s3.PutBucketVersioningInput, ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	return &s3.PutBucketVersioningOutput{}, nil
}

func (m *cmdS3Mock) PutPublicAccessBlock(context.Context, *s3.PutPublicAccessBlockInput, ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error) {
	return &s3.PutPublicAccessBlockOutput{}, nil
}

func (m *cmdS3Mock) PutBucketTagging(context.Context, *s3.PutBucketTaggingInput, ...func(*s3.Options)) (*s3.PutBucketTaggingOutput, error) {
	return &s3.PutBucketTaggingOutput{}, nil
}

func (m *cmdS3Mock) ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	return &s3.ListObjectVersionsOutput{}, nil
}

func (m *cmdS3Mock) DeleteObjects(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	return &s3.DeleteObjectsOutput{}, nil
}

func (m *cmdS3Mock) DeleteBucket(context.Context, *s3.DeleteBucketInput, ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	return &s3.DeleteBucketOutput{}, nil
}

type cmdSNSMock struct {
	listTopicsErr        error
	getTopicAttributesFn func(*sns.GetTopicAttributesInput) (*sns.GetTopicAttributesOutput, error)
}

func (m *cmdSNSMock) CreateTopic(context.Context, *sns.CreateTopicInput, ...func(*sns.Options)) (*sns.CreateTopicOutput, error) {
	return &sns.CreateTopicOutput{}, nil
}

func (m *cmdSNSMock) ListTopics(context.Context, *sns.ListTopicsInput, ...func(*sns.Options)) (*sns.ListTopicsOutput, error) {
	return &sns.ListTopicsOutput{}, m.listTopicsErr
}

func (m *cmdSNSMock) Publish(context.Context, *sns.PublishInput, ...func(*sns.Options)) (*sns.PublishOutput, error) {
	return &sns.PublishOutput{}, nil
}

func (m *cmdSNSMock) TagResource(context.Context, *sns.TagResourceInput, ...func(*sns.Options)) (*sns.TagResourceOutput, error) {
	return &sns.TagResourceOutput{}, nil
}

func (m *cmdSNSMock) GetTopicAttributes(_ context.Context, in *sns.GetTopicAttributesInput, _ ...func(*sns.Options)) (*sns.GetTopicAttributesOutput, error) {
	if m.getTopicAttributesFn != nil {
		return m.getTopicAttributesFn(in)
	}
	return nil, &snstypes.NotFoundException{}
}

func (m *cmdSNSMock) SetTopicAttributes(context.Context, *sns.SetTopicAttributesInput, ...func(*sns.Options)) (*sns.SetTopicAttributesOutput, error) {
	return &sns.SetTopicAttributesOutput{}, nil
}

func (m *cmdSNSMock) DeleteTopic(context.Context, *sns.DeleteTopicInput, ...func(*sns.Options)) (*sns.DeleteTopicOutput, error) {
	return &sns.DeleteTopicOutput{}, nil
}

type cmdBudgetsMock struct {
	describeBudgetFn   func(*budgets.DescribeBudgetInput) (*budgets.DescribeBudgetOutput, error)
	describeBudgetsErr error
}

func (m *cmdBudgetsMock) CreateBudget(context.Context, *budgets.CreateBudgetInput, ...func(*budgets.Options)) (*budgets.CreateBudgetOutput, error) {
	return &budgets.CreateBudgetOutput{}, nil
}

func (m *cmdBudgetsMock) DescribeBudget(_ context.Context, in *budgets.DescribeBudgetInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetOutput, error) {
	if m.describeBudgetFn != nil {
		return m.describeBudgetFn(in)
	}
	return nil, &budgetstypes.NotFoundException{}
}

func (m *cmdBudgetsMock) DescribeBudgets(context.Context, *budgets.DescribeBudgetsInput, ...func(*budgets.Options)) (*budgets.DescribeBudgetsOutput, error) {
	return &budgets.DescribeBudgetsOutput{}, m.describeBudgetsErr
}

func (m *cmdBudgetsMock) DeleteBudget(context.Context, *budgets.DeleteBudgetInput, ...func(*budgets.Options)) (*budgets.DeleteBudgetOutput, error) {
	return &budgets.DeleteBudgetOutput{}, nil
}

func resourceNotFoundTable() error {
	return &dbtypes.ResourceNotFoundException{}
}
