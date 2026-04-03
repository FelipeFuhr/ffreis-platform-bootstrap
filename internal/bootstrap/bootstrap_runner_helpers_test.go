package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
)

const (
	errRunnerNotImplemented = "not implemented"
	testRunnerRootARN       = "arn:aws:iam::123:root"
	testRunnerRegion        = "us-east-1"
)

type fakeDynamoDB struct {
	putInputs []*dynamodb.PutItemInput
	putErr    error
}

func (f *fakeDynamoDB) DescribeTable(context.Context, *dynamodb.DescribeTableInput, ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return nil, errors.New(errRunnerNotImplemented)
}
func (f *fakeDynamoDB) ListTables(context.Context, *dynamodb.ListTablesInput, ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return nil, errors.New(errRunnerNotImplemented)
}
func (f *fakeDynamoDB) CreateTable(context.Context, *dynamodb.CreateTableInput, ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	return nil, errors.New(errRunnerNotImplemented)
}
func (f *fakeDynamoDB) TagResource(context.Context, *dynamodb.TagResourceInput, ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
	return nil, errors.New(errRunnerNotImplemented)
}
func (f *fakeDynamoDB) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.putInputs = append(f.putInputs, params)
	if f.putErr != nil {
		return nil, f.putErr
	}
	return &dynamodb.PutItemOutput{}, nil
}
func (f *fakeDynamoDB) Scan(context.Context, *dynamodb.ScanInput, ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	return nil, errors.New(errRunnerNotImplemented)
}
func (f *fakeDynamoDB) DeleteTable(context.Context, *dynamodb.DeleteTableInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error) {
	return nil, errors.New(errRunnerNotImplemented)
}

type fakeSNS struct {
	publishErr error
}

func (f *fakeSNS) CreateTopic(context.Context, *sns.CreateTopicInput, ...func(*sns.Options)) (*sns.CreateTopicOutput, error) {
	return nil, errors.New(errRunnerNotImplemented)
}
func (f *fakeSNS) ListTopics(context.Context, *sns.ListTopicsInput, ...func(*sns.Options)) (*sns.ListTopicsOutput, error) {
	return nil, errors.New(errRunnerNotImplemented)
}
func (f *fakeSNS) Publish(context.Context, *sns.PublishInput, ...func(*sns.Options)) (*sns.PublishOutput, error) {
	return nil, f.publishErr
}
func (f *fakeSNS) TagResource(context.Context, *sns.TagResourceInput, ...func(*sns.Options)) (*sns.TagResourceOutput, error) {
	return &sns.TagResourceOutput{}, nil
}
func (f *fakeSNS) GetTopicAttributes(context.Context, *sns.GetTopicAttributesInput, ...func(*sns.Options)) (*sns.GetTopicAttributesOutput, error) {
	return nil, &snstypes.NotFoundException{}
}
func (f *fakeSNS) SetTopicAttributes(context.Context, *sns.SetTopicAttributesInput, ...func(*sns.Options)) (*sns.SetTopicAttributesOutput, error) {
	return &sns.SetTopicAttributesOutput{}, nil
}
func (f *fakeSNS) DeleteTopic(context.Context, *sns.DeleteTopicInput, ...func(*sns.Options)) (*sns.DeleteTopicOutput, error) {
	return &sns.DeleteTopicOutput{}, nil
}

func TestBootstrapRunnerEnsureResourcePostEnsureRuns(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	db := &fakeDynamoDB{}
	clients := &platformaws.Clients{
		DynamoDB:  db,
		AccountID: "123456789012",
		CallerARN: testRunnerRootARN,
		Region:    testRunnerRegion,
	}

	r := newBootstrapRunner(context.Background(), cfg, clients)

	called := false
	existed := true
	if err := r.ensureResource(context.Background(), ResourceTypeDynamoDBTable, "t", &existed, func(context.Context) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("ensureResource: %v", err)
	}
	if !called {
		t.Fatal("expected ensure fn to be called")
	}
	if len(db.putInputs) != 1 {
		t.Fatalf("expected 1 PutItem call (registry record), got %d", len(db.putInputs))
	}
	if db.putInputs[0] == nil || db.putInputs[0].ConditionExpression == nil {
		t.Fatal("expected PutItem to include ConditionExpression for registry record")
	}
}

func TestBootstrapRunnerEnsureResourceErrorDoesNotPostEnsure(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	db := &fakeDynamoDB{}
	clients := &platformaws.Clients{
		DynamoDB:  db,
		AccountID: "123456789012",
		CallerARN: testRunnerRootARN,
		Region:    testRunnerRegion,
	}

	r := newBootstrapRunner(context.Background(), cfg, clients)

	called := false
	existed := false
	err := r.ensureResource(context.Background(), ResourceTypeDynamoDBTable, "t", &existed, func(context.Context) error {
		called = true
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !called {
		t.Fatal("expected ensure fn to be called")
	}
	if len(db.putInputs) != 0 {
		t.Fatalf("expected no registry writes when ensure fails, got %d", len(db.putInputs))
	}
}

func TestBootstrapRunnerTryPublishNoTopicDoesNothing(t *testing.T) {
	t.Parallel()

	r := &bootstrapRunner{}
	r.tryPublish(context.Background(), platformaws.Event{})
}

func TestBootstrapRunnerExistedOrUnknown(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	clients := &platformaws.Clients{
		CallerARN: testRunnerRootARN,
	}
	r := newBootstrapRunner(context.Background(), cfg, clients)

	gotNil := r.existedOrUnknown(context.Background(), ResourceTypeS3Bucket, "b", func(context.Context, string) (bool, error) {
		return false, errors.New("boom")
	})
	if gotNil != nil {
		t.Fatal("expected nil existed on check error")
	}

	got := r.existedOrUnknown(context.Background(), ResourceTypeS3Bucket, "b", func(context.Context, string) (bool, error) {
		return true, nil
	})
	if got == nil || *got != true {
		t.Fatalf("expected existed=true pointer, got %v", got)
	}
}

func TestBootstrapRunnerRequireTopic(t *testing.T) {
	t.Parallel()

	r := &bootstrapRunner{}
	if err := r.requireTopic("x"); err == nil {
		t.Fatal("expected error when topic is missing")
	}
	r.topic = "arn"
	if err := r.requireTopic("x"); err != nil {
		t.Fatalf("expected nil when topic present, got %v", err)
	}
}

func TestBootstrapRunnerTryPublishAndRegisterErrorPathsDoNotFail(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	db := &fakeDynamoDB{putErr: errors.New("dynamo down")}
	sn := &fakeSNS{publishErr: errors.New("sns down")}
	clients := &platformaws.Clients{
		DynamoDB:  db,
		SNS:       sn,
		AccountID: "123456789012",
		CallerARN: testRunnerRootARN,
		Region:    testRunnerRegion,
	}

	r := newBootstrapRunner(context.Background(), cfg, clients)
	r.topic = "arn:aws:sns:us-east-1:123456789012:test"

	// Ensure error branches are exercised but the helpers keep going.
	r.tryPublish(context.Background(), platformaws.NewEvent(platformaws.EventTypeResourceEnsured, "X", "Y", clients.CallerARN))
	r.tryRegister(context.Background(), ResourceTypeIAMRole, "role")
}

func TestRunAccountConfigWritesAccountAndAdmin(t *testing.T) {
	t.Parallel()

	cfg := minimalConfig()
	cfg.Accounts = map[string]string{
		"dev":  "dev@example.com",
		"prod": "prod@example.com",
	}
	cfg.AdminEmail = "admin@example.com"

	db := &fakeDynamoDB{}
	clients := &platformaws.Clients{
		DynamoDB:  db,
		CallerARN: testRunnerRootARN,
	}
	r := newBootstrapRunner(context.Background(), cfg, clients)

	if err := runAccountConfig(context.Background(), r, cfg); err != nil {
		t.Fatalf("runAccountConfig: %v", err)
	}

	// 2 accounts + 1 admin config.
	if len(db.putInputs) != 3 {
		t.Fatalf("expected 3 PutItem calls, got %d", len(db.putInputs))
	}

	// Basic sanity: all puts target the registry table.
	for i, in := range db.putInputs {
		if in == nil || in.TableName == nil || *in.TableName == "" {
			t.Fatalf("put[%d] missing table name", i)
		}
	}
}
