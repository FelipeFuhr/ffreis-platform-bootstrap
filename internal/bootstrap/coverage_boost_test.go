package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
	"github.com/ffreis/platform-bootstrap/internal/logging"

	"github.com/aws/aws-sdk-go-v2/service/budgets"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type okS3 struct {
	headCalls          int
	listVersionsCalls  int
	deleteObjectsCalls int
	deleteBucketCalls  int
}

func (o *okS3) HeadBucket(_ context.Context, _ *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	o.headCalls++
	return &s3.HeadBucketOutput{}, nil
}
func (o *okS3) ListBuckets(_ context.Context, _ *s3.ListBucketsInput, _ ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	return &s3.ListBucketsOutput{}, nil
}
func (o *okS3) CreateBucket(_ context.Context, _ *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return &s3.CreateBucketOutput{}, nil
}
func (o *okS3) PutBucketVersioning(_ context.Context, _ *s3.PutBucketVersioningInput, _ ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	return &s3.PutBucketVersioningOutput{}, nil
}
func (o *okS3) PutPublicAccessBlock(_ context.Context, _ *s3.PutPublicAccessBlockInput, _ ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error) {
	return &s3.PutPublicAccessBlockOutput{}, nil
}
func (o *okS3) PutBucketTagging(_ context.Context, _ *s3.PutBucketTaggingInput, _ ...func(*s3.Options)) (*s3.PutBucketTaggingOutput, error) {
	return &s3.PutBucketTaggingOutput{}, nil
}
func (o *okS3) ListObjectVersions(_ context.Context, _ *s3.ListObjectVersionsInput, _ ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	o.listVersionsCalls++
	return &s3.ListObjectVersionsOutput{IsTruncated: nil}, nil
}
func (o *okS3) DeleteObjects(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	o.deleteObjectsCalls++
	return &s3.DeleteObjectsOutput{}, nil
}
func (o *okS3) DeleteBucket(_ context.Context, _ *s3.DeleteBucketInput, _ ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	o.deleteBucketCalls++
	return &s3.DeleteBucketOutput{}, nil
}

type okDynamoDB struct {
	deleteCalls int
	putItemErr  error
}

func (o *okDynamoDB) DescribeTable(_ context.Context, _ *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return &dynamodb.DescribeTableOutput{}, nil
}
func (o *okDynamoDB) ListTables(_ context.Context, _ *dynamodb.ListTablesInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return &dynamodb.ListTablesOutput{}, nil
}
func (o *okDynamoDB) CreateTable(_ context.Context, _ *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	return &dynamodb.CreateTableOutput{}, nil
}
func (o *okDynamoDB) TagResource(_ context.Context, _ *dynamodb.TagResourceInput, _ ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
	return &dynamodb.TagResourceOutput{}, nil
}
func (o *okDynamoDB) PutItem(_ context.Context, _ *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if o.putItemErr != nil {
		return nil, o.putItemErr
	}
	return &dynamodb.PutItemOutput{}, nil
}
func (o *okDynamoDB) Scan(_ context.Context, _ *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	return &dynamodb.ScanOutput{}, nil
}
func (o *okDynamoDB) DeleteTable(_ context.Context, _ *dynamodb.DeleteTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error) {
	o.deleteCalls++
	return &dynamodb.DeleteTableOutput{}, nil
}

type okIAM struct {
	roleExists        bool
	deleteRoleCalls   int
	listCalls         int
	deletePolicyCalls int
}

func (o *okIAM) GetRole(_ context.Context, _ *iam.GetRoleInput, _ ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	if !o.roleExists {
		return nil, errors.New("no such role")
	}
	return &iam.GetRoleOutput{}, nil
}
func (o *okIAM) GetAccountSummary(_ context.Context, _ *iam.GetAccountSummaryInput, _ ...func(*iam.Options)) (*iam.GetAccountSummaryOutput, error) {
	return &iam.GetAccountSummaryOutput{}, nil
}
func (o *okIAM) CreateRole(_ context.Context, _ *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	o.roleExists = true
	return &iam.CreateRoleOutput{}, nil
}
func (o *okIAM) PutRolePolicy(_ context.Context, _ *iam.PutRolePolicyInput, _ ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	return &iam.PutRolePolicyOutput{}, nil
}
func (o *okIAM) TagRole(_ context.Context, _ *iam.TagRoleInput, _ ...func(*iam.Options)) (*iam.TagRoleOutput, error) {
	return &iam.TagRoleOutput{}, nil
}
func (o *okIAM) ListRolePolicies(_ context.Context, _ *iam.ListRolePoliciesInput, _ ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error) {
	o.listCalls++
	return &iam.ListRolePoliciesOutput{PolicyNames: []string{"p1"}}, nil
}
func (o *okIAM) DeleteRolePolicy(_ context.Context, _ *iam.DeleteRolePolicyInput, _ ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
	o.deletePolicyCalls++
	return &iam.DeleteRolePolicyOutput{}, nil
}
func (o *okIAM) DeleteRole(_ context.Context, _ *iam.DeleteRoleInput, _ ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
	o.deleteRoleCalls++
	o.roleExists = false
	return &iam.DeleteRoleOutput{}, nil
}
func (o *okIAM) GetUser(_ context.Context, _ *iam.GetUserInput, _ ...func(*iam.Options)) (*iam.GetUserOutput, error) {
	return nil, errors.New("no such user")
}
func (o *okIAM) CreateUser(_ context.Context, _ *iam.CreateUserInput, _ ...func(*iam.Options)) (*iam.CreateUserOutput, error) {
	return &iam.CreateUserOutput{}, nil
}
func (o *okIAM) PutUserPolicy(_ context.Context, _ *iam.PutUserPolicyInput, _ ...func(*iam.Options)) (*iam.PutUserPolicyOutput, error) {
	return &iam.PutUserPolicyOutput{}, nil
}
func (o *okIAM) CreateAccessKey(_ context.Context, _ *iam.CreateAccessKeyInput, _ ...func(*iam.Options)) (*iam.CreateAccessKeyOutput, error) {
	return &iam.CreateAccessKeyOutput{}, nil
}
func (o *okIAM) ListAccessKeys(_ context.Context, _ *iam.ListAccessKeysInput, _ ...func(*iam.Options)) (*iam.ListAccessKeysOutput, error) {
	return &iam.ListAccessKeysOutput{}, nil
}
func (o *okIAM) DeleteAccessKey(_ context.Context, _ *iam.DeleteAccessKeyInput, _ ...func(*iam.Options)) (*iam.DeleteAccessKeyOutput, error) {
	return &iam.DeleteAccessKeyOutput{}, nil
}
func (o *okIAM) DeleteUserPolicy(_ context.Context, _ *iam.DeleteUserPolicyInput, _ ...func(*iam.Options)) (*iam.DeleteUserPolicyOutput, error) {
	return &iam.DeleteUserPolicyOutput{}, nil
}
func (o *okIAM) DeleteUser(_ context.Context, _ *iam.DeleteUserInput, _ ...func(*iam.Options)) (*iam.DeleteUserOutput, error) {
	return &iam.DeleteUserOutput{}, nil
}

type okSNS struct {
	publishCalls int
	publishErr   error
	deleteCalls  int
}

func (o *okSNS) CreateTopic(_ context.Context, _ *sns.CreateTopicInput, _ ...func(*sns.Options)) (*sns.CreateTopicOutput, error) {
	return &sns.CreateTopicOutput{}, nil
}
func (o *okSNS) ListTopics(_ context.Context, _ *sns.ListTopicsInput, _ ...func(*sns.Options)) (*sns.ListTopicsOutput, error) {
	return &sns.ListTopicsOutput{}, nil
}
func (o *okSNS) Publish(_ context.Context, _ *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	o.publishCalls++
	return &sns.PublishOutput{}, o.publishErr
}
func (o *okSNS) TagResource(_ context.Context, _ *sns.TagResourceInput, _ ...func(*sns.Options)) (*sns.TagResourceOutput, error) {
	return &sns.TagResourceOutput{}, nil
}
func (o *okSNS) GetTopicAttributes(_ context.Context, _ *sns.GetTopicAttributesInput, _ ...func(*sns.Options)) (*sns.GetTopicAttributesOutput, error) {
	return &sns.GetTopicAttributesOutput{Attributes: map[string]string{}}, nil
}
func (o *okSNS) SetTopicAttributes(_ context.Context, _ *sns.SetTopicAttributesInput, _ ...func(*sns.Options)) (*sns.SetTopicAttributesOutput, error) {
	return &sns.SetTopicAttributesOutput{}, nil
}
func (o *okSNS) DeleteTopic(_ context.Context, _ *sns.DeleteTopicInput, _ ...func(*sns.Options)) (*sns.DeleteTopicOutput, error) {
	o.deleteCalls++
	return &sns.DeleteTopicOutput{}, nil
}

type okBudgets struct {
	deleteCalls int
	deleteErr   error
}

func (o *okBudgets) CreateBudget(_ context.Context, _ *budgets.CreateBudgetInput, _ ...func(*budgets.Options)) (*budgets.CreateBudgetOutput, error) {
	return &budgets.CreateBudgetOutput{}, nil
}
func (o *okBudgets) DescribeBudget(_ context.Context, _ *budgets.DescribeBudgetInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetOutput, error) {
	return &budgets.DescribeBudgetOutput{}, nil
}
func (o *okBudgets) DescribeBudgets(_ context.Context, _ *budgets.DescribeBudgetsInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetsOutput, error) {
	return &budgets.DescribeBudgetsOutput{}, nil
}
func (o *okBudgets) DeleteBudget(_ context.Context, _ *budgets.DeleteBudgetInput, _ ...func(*budgets.Options)) (*budgets.DeleteBudgetOutput, error) {
	o.deleteCalls++
	return &budgets.DeleteBudgetOutput{}, o.deleteErr
}

func TestValidateClientsForBootstrapSuccess(t *testing.T) {
	var typedNilS3 *s3.Client
	var typedNilDB *dynamodb.Client
	var typedNilIAM *iam.Client
	var typedNilSNS *sns.Client
	var typedNilBudgets *budgets.Client

	clients := &platformaws.Clients{
		S3:        typedNilS3,
		DynamoDB:  typedNilDB,
		IAM:       typedNilIAM,
		SNS:       typedNilSNS,
		Budgets:   typedNilBudgets,
		AccountID: "123",
		CallerARN: testCallerARN,
		Region:    testRegion,
	}

	if err := validateClientsForBootstrap(clients); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}
}

func TestValidateClientsForBootstrapMissingFields(t *testing.T) {
	err := validateClientsForBootstrap(&platformaws.Clients{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestValidateClientsForNukeSuccess(t *testing.T) {
	var typedNilS3 *s3.Client
	var typedNilDB *dynamodb.Client
	var typedNilIAM *iam.Client
	var typedNilSNS *sns.Client
	var typedNilBudgets *budgets.Client

	clients := &platformaws.Clients{
		S3:        typedNilS3,
		DynamoDB:  typedNilDB,
		IAM:       typedNilIAM,
		SNS:       typedNilSNS,
		Budgets:   typedNilBudgets,
		AccountID: "123",
		Region:    testRegion,
	}

	if err := validateClientsForNuke(clients); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}
}

func TestValidateClientsForNukeMissingFields(t *testing.T) {
	err := validateClientsForNuke(&platformaws.Clients{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBootstrapRunnerTryPublishPublishErrorStillContinues(t *testing.T) {
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(testSink{}, nil)))

	snsMock := &okSNS{publishErr: errors.New("publish failed")}
	r := &bootstrapRunner{
		cfg: &config.Config{OrgName: "acme"},
		c: &platformaws.Clients{
			SNS:       snsMock,
			CallerARN: testCallerARN,
		},
		log:   slog.Default(),
		topic: "arn:aws:sns:us-east-1:123:topic",
	}

	r.tryPublish(ctx, platformaws.NewEvent(platformaws.EventTypeResourceCreated, "S3Bucket", "b", r.c.CallerARN))

	if snsMock.publishCalls != 1 {
		t.Errorf("publishCalls: want 1, got %d", snsMock.publishCalls)
	}
}

func TestBootstrapRunnerTryRegisterRegisterErrorStillContinues(t *testing.T) {
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(testSink{}, nil)))

	dbMock := &okDynamoDB{putItemErr: errors.New("put failed")}
	r := &bootstrapRunner{
		cfg: &config.Config{OrgName: "acme"},
		c: &platformaws.Clients{
			DynamoDB:  dbMock,
			CallerARN: testCallerARN,
		},
		log:           slog.Default(),
		tags:          platformaws.RequiredTags("acme", "dev"),
		registryTable: "registry",
	}

	r.tryRegister(ctx, ResourceTypeS3Bucket, "bucket")
}

func TestNukeNonDryRunSuccessRunsAllSteps(t *testing.T) {
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(testSink{}, nil)))

	cfg := &config.Config{
		OrgName:          "acme",
		Region:           testRegion,
		StateRegion:      testRegion,
		LogLevel:         "info",
		BudgetMonthlyUSD: 20.0,
		Accounts:         map[string]string{},
		DryRun:           false,
	}

	s3Mock := &okS3{}
	dbMock := &okDynamoDB{}
	iamMock := &okIAM{roleExists: true}
	snsMock := &okSNS{}
	budMock := &okBudgets{}

	clients := &platformaws.Clients{
		S3:        s3Mock,
		DynamoDB:  dbMock,
		IAM:       iamMock,
		SNS:       snsMock,
		Budgets:   budMock,
		AccountID: "123456789012",
		Region:    testRegion,
	}

	if err := Nuke(ctx, cfg, clients); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}

	if budMock.deleteCalls != 1 {
		t.Errorf("DeleteBudget calls: want 1, got %d", budMock.deleteCalls)
	}
	if snsMock.deleteCalls != 1 {
		t.Errorf("DeleteTopic calls: want 1, got %d", snsMock.deleteCalls)
	}
	if iamMock.deleteRoleCalls != 1 {
		t.Errorf("DeleteRole calls: want 1, got %d", iamMock.deleteRoleCalls)
	}
	if dbMock.deleteCalls != 2 {
		t.Errorf("DeleteTable calls: want 2, got %d", dbMock.deleteCalls)
	}
	if s3Mock.deleteBucketCalls != 1 {
		t.Errorf("DeleteBucket calls: want 1, got %d", s3Mock.deleteBucketCalls)
	}
}

func TestNukeContinuesOnError(t *testing.T) {
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(testSink{}, nil)))

	cfg := &config.Config{
		OrgName:          "acme",
		Region:           testRegion,
		StateRegion:      testRegion,
		LogLevel:         "info",
		BudgetMonthlyUSD: 20.0,
		Accounts:         map[string]string{},
		DryRun:           false,
	}

	s3Mock := &okS3{}
	dbMock := &okDynamoDB{}
	iamMock := &okIAM{roleExists: true}
	snsMock := &okSNS{}
	budMock := &okBudgets{deleteErr: errors.New("budget delete failed")}

	clients := &platformaws.Clients{
		S3:        s3Mock,
		DynamoDB:  dbMock,
		IAM:       iamMock,
		SNS:       snsMock,
		Budgets:   budMock,
		AccountID: "123456789012",
		Region:    testRegion,
	}

	if err := Nuke(ctx, cfg, clients); err == nil {
		t.Fatal("expected aggregated error, got nil")
	}

	// Even with an early failure, later steps are still attempted.
	if dbMock.deleteCalls != 2 {
		t.Errorf("DeleteTable calls: want 2, got %d", dbMock.deleteCalls)
	}
	if s3Mock.deleteBucketCalls != 1 {
		t.Errorf("DeleteBucket calls: want 1, got %d", s3Mock.deleteBucketCalls)
	}
}

// testSink is an io.Writer that discards output; it avoids importing io
// in this file and keeps log wiring explicit in tests.
type testSink struct{}

func (testSink) Write(p []byte) (int, error) { return len(p), nil }
