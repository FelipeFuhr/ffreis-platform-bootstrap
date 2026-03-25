//go:build integration

package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
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
	"github.com/ffreis/platform-bootstrap/internal/config"
)

func integrationConfig() *config.Config {
	return &config.Config{
		OrgName:          "acme",
		Region:           "us-east-1",
		StateRegion:      "us-east-1",
		LogLevel:         "info",
		BudgetMonthlyUSD: 25.0,
		Accounts: map[string]string{
			"dev": "dev@example.com",
		},
	}
}

// integrationMockS3 implements platformaws.S3API.
type integrationMockS3 struct {
	bucketExists     bool
	createCalls      int
	versioningCalls  int
	publicBlockCalls int
	tagCalls         int
	versioningErr    error
}

func (m *integrationMockS3) HeadBucket(_ context.Context, _ *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.bucketExists {
		return &s3.HeadBucketOutput{}, nil
	}
	return nil, &s3types.NotFound{}
}
func (m *integrationMockS3) CreateBucket(_ context.Context, _ *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	m.createCalls++
	m.bucketExists = true
	return &s3.CreateBucketOutput{}, nil
}
func (m *integrationMockS3) PutBucketVersioning(_ context.Context, _ *s3.PutBucketVersioningInput, _ ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	m.versioningCalls++
	return &s3.PutBucketVersioningOutput{}, m.versioningErr
}
func (m *integrationMockS3) PutPublicAccessBlock(_ context.Context, _ *s3.PutPublicAccessBlockInput, _ ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error) {
	m.publicBlockCalls++
	return &s3.PutPublicAccessBlockOutput{}, nil
}
func (m *integrationMockS3) PutBucketTagging(_ context.Context, _ *s3.PutBucketTaggingInput, _ ...func(*s3.Options)) (*s3.PutBucketTaggingOutput, error) {
	m.tagCalls++
	return &s3.PutBucketTaggingOutput{}, nil
}

// integrationMockDynamoDB implements platformaws.DynamoDBAPI.
type integrationMockDynamoDB struct {
	tables           map[string]dbtypes.TableStatus
	createTableCalls int
	tagCalls         int
	putItemCalls     int
}

func (m *integrationMockDynamoDB) DescribeTable(_ context.Context, in *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	if m.tables == nil {
		m.tables = map[string]dbtypes.TableStatus{}
	}
	name := sdkaws.ToString(in.TableName)
	status, ok := m.tables[name]
	if !ok {
		return nil, &dbtypes.ResourceNotFoundException{}
	}
	return &dynamodb.DescribeTableOutput{Table: &dbtypes.TableDescription{
		TableStatus: status,
		TableArn:    sdkaws.String("arn:aws:dynamodb:us-east-1:123456789012:table/" + name),
	}}, nil
}
func (m *integrationMockDynamoDB) CreateTable(_ context.Context, in *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	m.createTableCalls++
	if m.tables == nil {
		m.tables = map[string]dbtypes.TableStatus{}
	}
	m.tables[sdkaws.ToString(in.TableName)] = dbtypes.TableStatusActive
	return &dynamodb.CreateTableOutput{}, nil
}
func (m *integrationMockDynamoDB) TagResource(_ context.Context, _ *dynamodb.TagResourceInput, _ ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
	m.tagCalls++
	return &dynamodb.TagResourceOutput{}, nil
}
func (m *integrationMockDynamoDB) PutItem(_ context.Context, _ *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	m.putItemCalls++
	return &dynamodb.PutItemOutput{}, nil
}
func (m *integrationMockDynamoDB) Scan(_ context.Context, _ *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	return &dynamodb.ScanOutput{}, nil
}

// integrationMockIAM implements platformaws.IAMAPI.
type integrationMockIAM struct {
	roleExists      bool
	createRoleCalls int
	putPolicyCalls  int
	tagCalls        int
}

func (m *integrationMockIAM) GetRole(_ context.Context, _ *iam.GetRoleInput, _ ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	if m.roleExists {
		return &iam.GetRoleOutput{Role: &iamtypes.Role{RoleName: sdkaws.String(config.RoleNamePlatformAdmin)}}, nil
	}
	return nil, &iamtypes.NoSuchEntityException{}
}
func (m *integrationMockIAM) CreateRole(_ context.Context, in *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	m.createRoleCalls++
	m.roleExists = true
	return &iam.CreateRoleOutput{Role: &iamtypes.Role{RoleName: in.RoleName}}, nil
}
func (m *integrationMockIAM) PutRolePolicy(_ context.Context, _ *iam.PutRolePolicyInput, _ ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	m.putPolicyCalls++
	return &iam.PutRolePolicyOutput{}, nil
}
func (m *integrationMockIAM) TagRole(_ context.Context, _ *iam.TagRoleInput, _ ...func(*iam.Options)) (*iam.TagRoleOutput, error) {
	m.tagCalls++
	return &iam.TagRoleOutput{}, nil
}

// integrationMockSNS implements platformaws.SNSAPI.
type integrationMockSNS struct {
	topicExists      bool
	topicARN         string
	createTopicCalls int
	publishCalls     int
	tagCalls         int
	setAttrCalls     int
	returnNilTopic   bool
}

func (m *integrationMockSNS) CreateTopic(_ context.Context, _ *sns.CreateTopicInput, _ ...func(*sns.Options)) (*sns.CreateTopicOutput, error) {
	m.createTopicCalls++
	m.topicExists = true
	if m.returnNilTopic {
		return &sns.CreateTopicOutput{}, nil
	}
	arn := m.topicARN
	if arn == "" {
		arn = "arn:aws:sns:us-east-1:123456789012:acme-platform-events"
	}
	m.topicARN = arn
	return &sns.CreateTopicOutput{TopicArn: sdkaws.String(arn)}, nil
}
func (m *integrationMockSNS) Publish(_ context.Context, _ *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	m.publishCalls++
	return &sns.PublishOutput{MessageId: sdkaws.String("msg-1")}, nil
}
func (m *integrationMockSNS) TagResource(_ context.Context, _ *sns.TagResourceInput, _ ...func(*sns.Options)) (*sns.TagResourceOutput, error) {
	m.tagCalls++
	return &sns.TagResourceOutput{}, nil
}
func (m *integrationMockSNS) GetTopicAttributes(_ context.Context, _ *sns.GetTopicAttributesInput, _ ...func(*sns.Options)) (*sns.GetTopicAttributesOutput, error) {
	if !m.topicExists {
		return nil, errors.New("topic not found")
	}
	return &sns.GetTopicAttributesOutput{Attributes: map[string]string{}}, nil
}
func (m *integrationMockSNS) SetTopicAttributes(_ context.Context, _ *sns.SetTopicAttributesInput, _ ...func(*sns.Options)) (*sns.SetTopicAttributesOutput, error) {
	m.setAttrCalls++
	return &sns.SetTopicAttributesOutput{}, nil
}

// integrationMockBudgets implements platformaws.BudgetsAPI.
type integrationMockBudgets struct {
	budgetExists bool
	createCalls  int
}

func (m *integrationMockBudgets) DescribeBudget(_ context.Context, _ *budgets.DescribeBudgetInput, _ ...func(*budgets.Options)) (*budgets.DescribeBudgetOutput, error) {
	if m.budgetExists {
		return &budgets.DescribeBudgetOutput{Budget: &budgetstypes.Budget{}}, nil
	}
	return nil, &budgetstypes.NotFoundException{}
}
func (m *integrationMockBudgets) CreateBudget(_ context.Context, _ *budgets.CreateBudgetInput, _ ...func(*budgets.Options)) (*budgets.CreateBudgetOutput, error) {
	m.createCalls++
	m.budgetExists = true
	return &budgets.CreateBudgetOutput{}, nil
}

func newIntegrationClients(s3c *integrationMockS3, dbc *integrationMockDynamoDB, iamc *integrationMockIAM, snsc *integrationMockSNS, budc *integrationMockBudgets) *platformaws.Clients {
	return &platformaws.Clients{
		S3:        s3c,
		DynamoDB:  dbc,
		IAM:       iamc,
		SNS:       snsc,
		Budgets:   budc,
		AccountID: "123456789012",
		CallerARN: "arn:aws:iam::123456789012:user/bootstrap",
		Region:    "us-east-1",
	}
}

func TestRun_HappyPath_Integration(t *testing.T) {
	cfg := integrationConfig()

	s3c := &integrationMockS3{}
	dbc := &integrationMockDynamoDB{}
	iamc := &integrationMockIAM{}
	snsc := &integrationMockSNS{}
	budc := &integrationMockBudgets{}
	clients := newIntegrationClients(s3c, dbc, iamc, snsc, budc)

	err := Run(context.Background(), cfg, clients)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if dbc.createTableCalls != 2 {
		t.Errorf("CreateTable calls: want 2 (registry+lock), got %d", dbc.createTableCalls)
	}
	if s3c.createCalls != 1 {
		t.Errorf("CreateBucket calls: want 1, got %d", s3c.createCalls)
	}
	if iamc.createRoleCalls != 1 {
		t.Errorf("CreateRole calls: want 1, got %d", iamc.createRoleCalls)
	}
	if snsc.createTopicCalls != 1 {
		t.Errorf("CreateTopic calls: want 1, got %d", snsc.createTopicCalls)
	}
	if snsc.setAttrCalls != 1 {
		t.Errorf("SetTopicAttributes calls: want 1, got %d", snsc.setAttrCalls)
	}
	if budc.createCalls != 1 {
		t.Errorf("CreateBudget calls: want 1, got %d", budc.createCalls)
	}
	if snsc.publishCalls != 2 {
		t.Errorf("Publish calls: want 2 events (topic + budget), got %d", snsc.publishCalls)
	}
	if dbc.putItemCalls != 7 {
		t.Errorf("PutItem calls: want 7 (6 resources + 1 account config), got %d", dbc.putItemCalls)
	}
}

func TestRun_Idempotent_Integration(t *testing.T) {
	cfg := integrationConfig()
	cfg.Accounts = map[string]string{}

	s3c := &integrationMockS3{bucketExists: true}
	dbc := &integrationMockDynamoDB{tables: map[string]dbtypes.TableStatus{
		cfg.RegistryTableName(): dbtypes.TableStatusActive,
		cfg.LockTableName():     dbtypes.TableStatusActive,
	}}
	iamc := &integrationMockIAM{roleExists: true}
	snsc := &integrationMockSNS{topicExists: true, topicARN: "arn:aws:sns:us-east-1:123456789012:acme-platform-events"}
	budc := &integrationMockBudgets{budgetExists: true}
	clients := newIntegrationClients(s3c, dbc, iamc, snsc, budc)

	err := Run(context.Background(), cfg, clients)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if s3c.createCalls != 0 {
		t.Errorf("CreateBucket calls: want 0 for existing bucket, got %d", s3c.createCalls)
	}
	if dbc.createTableCalls != 0 {
		t.Errorf("CreateTable calls: want 0 for existing tables, got %d", dbc.createTableCalls)
	}
	if iamc.createRoleCalls != 0 {
		t.Errorf("CreateRole calls: want 0 for existing role, got %d", iamc.createRoleCalls)
	}
	// EnsureEventsTopic uses CreateTopic idempotently on every run.
	if snsc.createTopicCalls != 1 {
		t.Errorf("CreateTopic calls: want 1, got %d", snsc.createTopicCalls)
	}
	if budc.createCalls != 0 {
		t.Errorf("CreateBudget calls: want 0 for existing budget, got %d", budc.createCalls)
	}
	if snsc.publishCalls != 2 {
		t.Errorf("Publish calls: want 2 events (topic + budget), got %d", snsc.publishCalls)
	}
	if dbc.putItemCalls != 6 {
		t.Errorf("PutItem calls: want 6 resource registrations, got %d", dbc.putItemCalls)
	}
}

func TestRun_StepFailure_Integration(t *testing.T) {
	cfg := integrationConfig()
	cfg.Accounts = map[string]string{}

	s3c := &integrationMockS3{versioningErr: errors.New("versioning failed")}
	dbc := &integrationMockDynamoDB{}
	iamc := &integrationMockIAM{}
	snsc := &integrationMockSNS{}
	budc := &integrationMockBudgets{}
	clients := newIntegrationClients(s3c, dbc, iamc, snsc, budc)

	err := Run(context.Background(), cfg, clients)
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "step state-bucket") {
		t.Fatalf("error should include failing step name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "enabling versioning") {
		t.Fatalf("error should preserve root cause context, got: %v", err)
	}
}

func TestRun_TopicARNGuard_Integration(t *testing.T) {
	cfg := integrationConfig()
	cfg.Accounts = map[string]string{}

	s3c := &integrationMockS3{}
	dbc := &integrationMockDynamoDB{}
	iamc := &integrationMockIAM{}
	snsc := &integrationMockSNS{returnNilTopic: true}
	budc := &integrationMockBudgets{}
	clients := newIntegrationClients(s3c, dbc, iamc, snsc, budc)

	err := Run(context.Background(), cfg, clients)
	if err == nil {
		t.Fatal("Run() expected guard error, got nil")
	}
	if !strings.Contains(err.Error(), "step platform-events-policy") {
		t.Fatalf("error should include failing step name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "requires platform-events-topic") {
		t.Fatalf("error should include topic ordering guard message, got: %v", err)
	}
}
