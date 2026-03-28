package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// newTestClients builds a *Clients with all the provided mocks. String fields
// are set to realistic-looking values so ARN construction in TopicExists works.
func newTestClients(s3 S3API, db DynamoDBAPI, iam IAMAPI, snsAPI SNSAPI, bud BudgetsAPI) *Clients {
	return &Clients{
		S3:        s3,
		DynamoDB:  db,
		IAM:       iam,
		SNS:       snsAPI,
		Budgets:   bud,
		AccountID: testAccountID,
		Region:    "us-east-1",
	}
}

// ── BucketExists ─────────────────────────────────────────────────────────────

func TestBucketExists_True(t *testing.T) {
	c := newTestClients(&mockS3{bucketExists: true}, nil, nil, nil, nil)
	if !c.BucketExists(context.Background(), testExistsBucket) {
		t.Error(errWantTrue)
	}
}

func TestBucketExists_False(t *testing.T) {
	c := newTestClients(&mockS3{bucketExists: false}, nil, nil, nil, nil)
	if c.BucketExists(context.Background(), testExistsBucket) {
		t.Error(errWantFalse)
	}
}

// ── TableExists ──────────────────────────────────────────────────────────────

func TestTableExists_ActiveTable(t *testing.T) {
	c := newTestClients(nil, &mockDynamoDB{tableStatus: dbtypes.TableStatusActive}, nil, nil, nil)
	if !c.TableExists(context.Background(), testExistsTable) {
		t.Error("want true for ACTIVE table, got false")
	}
}

func TestTableExists_NonActiveTable(t *testing.T) {
	c := newTestClients(nil, &mockDynamoDB{tableStatus: dbtypes.TableStatusCreating}, nil, nil, nil)
	if c.TableExists(context.Background(), testExistsTable) {
		t.Error("want false for non-ACTIVE table, got true")
	}
}

func TestTableExists_TableNotFound(t *testing.T) {
	// tableStatus == "" → DescribeTable returns ResourceNotFoundException
	c := newTestClients(nil, &mockDynamoDB{tableStatus: ""}, nil, nil, nil)
	if c.TableExists(context.Background(), "missing-table") {
		t.Error("want false for non-existent table, got true")
	}
}

func TestTableExistsChecked_True(t *testing.T) {
	c := newTestClients(nil, &mockDynamoDB{tableStatus: dbtypes.TableStatusActive}, nil, nil, nil)
	ok, err := c.TableExistsChecked(context.Background(), testExistsTable)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error(errWantTrue)
	}
}

func TestTableExistsChecked_NotFoundIsNil(t *testing.T) {
	c := newTestClients(nil, &mockDynamoDB{tableStatus: ""}, nil, nil, nil)
	ok, err := c.TableExistsChecked(context.Background(), "missing-table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error(errWantFalse)
	}
}

type erringDynamoDB struct{}

func (e *erringDynamoDB) DescribeTable(_ context.Context, _ *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return nil, errors.New("boom")
}
func (e *erringDynamoDB) ListTables(_ context.Context, _ *dynamodb.ListTablesInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return &dynamodb.ListTablesOutput{}, nil
}
func (e *erringDynamoDB) CreateTable(_ context.Context, _ *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	return &dynamodb.CreateTableOutput{}, nil
}
func (e *erringDynamoDB) TagResource(_ context.Context, _ *dynamodb.TagResourceInput, _ ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
	return &dynamodb.TagResourceOutput{}, nil
}
func (e *erringDynamoDB) PutItem(_ context.Context, _ *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return &dynamodb.PutItemOutput{}, nil
}
func (e *erringDynamoDB) Scan(_ context.Context, _ *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	return &dynamodb.ScanOutput{}, nil
}
func (e *erringDynamoDB) DeleteTable(_ context.Context, _ *dynamodb.DeleteTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error) {
	return &dynamodb.DeleteTableOutput{}, nil
}

func TestTableExistsChecked_UnexpectedError(t *testing.T) {
	c := newTestClients(nil, &erringDynamoDB{}, nil, nil, nil)
	_, err := c.TableExistsChecked(context.Background(), testExistsTable)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── RoleExists ───────────────────────────────────────────────────────────────

func TestRoleExists_True(t *testing.T) {
	c := newTestClients(nil, nil, &mockIAM{roleExists: true}, nil, nil)
	if !c.RoleExists(context.Background(), testRoleName) {
		t.Error(errWantTrue)
	}
}

func TestRoleExists_False(t *testing.T) {
	c := newTestClients(nil, nil, &mockIAM{roleExists: false}, nil, nil)
	if c.RoleExists(context.Background(), testRoleName) {
		t.Error(errWantFalse)
	}
}

// ── TopicExists ──────────────────────────────────────────────────────────────

func TestTopicExists_True(t *testing.T) {
	// mockSNS.GetTopicAttributes always returns nil error → topic exists.
	c := newTestClients(nil, nil, nil, &mockSNS{}, nil)
	if !c.TopicExists(context.Background(), testExistsTopicName) {
		t.Error(errWantTrue)
	}
}

// erringSNS wraps mockSNS and overrides GetTopicAttributes to return an error.
type erringSNS struct {
	*mockSNS
}

func (e *erringSNS) GetTopicAttributes(_ context.Context, _ *sns.GetTopicAttributesInput, _ ...func(*sns.Options)) (*sns.GetTopicAttributesOutput, error) {
	return nil, errors.New("topic not found")
}

func TestTopicExists_False(t *testing.T) {
	c := newTestClients(nil, nil, nil, &erringSNS{&mockSNS{}}, nil)
	if c.TopicExists(context.Background(), testExistsTopicName) {
		t.Error("want false when GetTopicAttributes errors, got true")
	}
}

// ── BudgetExists ─────────────────────────────────────────────────────────────

func TestBudgetExists_True(t *testing.T) {
	c := newTestClients(nil, nil, nil, nil, &mockBudgets{budgetExists: true})
	if !c.BudgetExists(context.Background(), testBudgetName) {
		t.Error(errWantTrue)
	}
}

func TestBudgetExists_False(t *testing.T) {
	c := newTestClients(nil, nil, nil, nil, &mockBudgets{budgetExists: false})
	if c.BudgetExists(context.Background(), testBudgetName) {
		t.Error(errWantFalse)
	}
}

// ── ResourceExists dispatch ───────────────────────────────────────────────────

func TestResourceExists_S3Bucket(t *testing.T) {
	c := newTestClients(&mockS3{bucketExists: true}, nil, nil, nil, nil)
	if !c.ResourceExists(context.Background(), "S3Bucket", testExistsBucket) {
		t.Error("S3Bucket dispatch: want true")
	}
}

func TestResourceExists_DynamoDBTable(t *testing.T) {
	c := newTestClients(nil, &mockDynamoDB{tableStatus: dbtypes.TableStatusActive}, nil, nil, nil)
	if !c.ResourceExists(context.Background(), "DynamoDBTable", testExistsTable) {
		t.Error("DynamoDBTable dispatch: want true")
	}
}

func TestResourceExists_IAMRole(t *testing.T) {
	c := newTestClients(nil, nil, &mockIAM{roleExists: true}, nil, nil)
	if !c.ResourceExists(context.Background(), "IAMRole", testRoleName) {
		t.Error("IAMRole dispatch: want true")
	}
}

func TestResourceExists_SNSTopic(t *testing.T) {
	c := newTestClients(nil, nil, nil, &mockSNS{}, nil)
	if !c.ResourceExists(context.Background(), "SNSTopic", testExistsTopicName) {
		t.Error("SNSTopic dispatch: want true")
	}
}

func TestResourceExists_AWSBudget(t *testing.T) {
	c := newTestClients(nil, nil, nil, nil, &mockBudgets{budgetExists: true})
	if !c.ResourceExists(context.Background(), "AWSBudget", testBudgetName) {
		t.Error("AWSBudget dispatch: want true")
	}
}

func TestResourceExists_UnknownType(t *testing.T) {
	c := newTestClients(nil, nil, nil, nil, nil)
	if c.ResourceExists(context.Background(), "KinesisStream", "my-stream") {
		t.Error("unknown resource type: want false, got true")
	}
}
