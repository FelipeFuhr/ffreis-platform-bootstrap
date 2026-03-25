package aws

import (
	"context"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func init() {
	// Remove the 3-second sleep between polls so tests run instantly.
	tableActivePollInterval = time.Millisecond
}

// mockDynamoDB is a stateful stand-in for DynamoDBAPI.
// tableStatus is empty string when the table does not exist.
// CreateTable transitions it to ACTIVE, mirroring real AWS behaviour.
type mockDynamoDB struct {
	tableStatus  dbtypes.TableStatus // empty = table does not exist
	createCalls  int
	createErr    error
	tagCalls     int
	tagErr       error
}

func (m *mockDynamoDB) ListTables(_ context.Context, _ *dynamodb.ListTablesInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return &dynamodb.ListTablesOutput{}, nil
}

func (m *mockDynamoDB) DescribeTable(_ context.Context, _ *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	if m.tableStatus == "" {
		return nil, &dbtypes.ResourceNotFoundException{}
	}
	return &dynamodb.DescribeTableOutput{
		Table: &dbtypes.TableDescription{
			TableStatus: m.tableStatus,
			TableArn:    sdkaws.String("arn:aws:dynamodb:us-east-1:123:table/test-table"),
		},
	}, nil
}

func (m *mockDynamoDB) CreateTable(_ context.Context, params *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	m.createCalls++
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.tableStatus = dbtypes.TableStatusActive
	return &dynamodb.CreateTableOutput{
		TableDescription: &dbtypes.TableDescription{
			TableName:   params.TableName,
			TableStatus: dbtypes.TableStatusActive,
			TableArn:    sdkaws.String("arn:aws:dynamodb:us-east-1:123:table/test-table"),
		},
	}, nil
}

func (m *mockDynamoDB) TagResource(_ context.Context, _ *dynamodb.TagResourceInput, _ ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
	m.tagCalls++
	return &dynamodb.TagResourceOutput{}, m.tagErr
}

func (m *mockDynamoDB) DeleteTable(_ context.Context, _ *dynamodb.DeleteTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error) {
	m.tableStatus = ""
	return &dynamodb.DeleteTableOutput{}, nil
}

func (m *mockDynamoDB) PutItem(_ context.Context, _ *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockDynamoDB) Scan(_ context.Context, _ *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	return &dynamodb.ScanOutput{}, nil
}

// TestEnsureLockTable_Create verifies that when the table does not exist,
// CreateTable is called and the result is an ACTIVE table.
func TestEnsureLockTable_Create(t *testing.T) {
	m := &mockDynamoDB{}

	if err := EnsureLockTable(context.Background(), m, "test-table", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.createCalls != 1 {
		t.Errorf("createCalls: want 1, got %d", m.createCalls)
	}
	if m.tableStatus != dbtypes.TableStatusActive {
		t.Errorf("tableStatus: want ACTIVE, got %s", m.tableStatus)
	}
	if m.tagCalls != 0 {
		t.Errorf("tagCalls: want 0 (nil tags), got %d", m.tagCalls)
	}
}

// TestEnsureLockTable_AlreadyActive verifies that when the table is already
// ACTIVE, CreateTable is never called.
func TestEnsureLockTable_AlreadyActive(t *testing.T) {
	m := &mockDynamoDB{tableStatus: dbtypes.TableStatusActive}

	if err := EnsureLockTable(context.Background(), m, "test-table", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.createCalls != 0 {
		t.Errorf("createCalls: want 0 (table already active), got %d", m.createCalls)
	}
}

// TestEnsureLockTable_ResourceInUse verifies that a concurrent CreateTable
// (ResourceInUseException) is treated as success.
func TestEnsureLockTable_ResourceInUse(t *testing.T) {
	concurrent := &concurrentMockDynamoDB{}
	if err := EnsureLockTable(context.Background(), concurrent, "test-table", nil); err != nil {
		t.Fatalf("expected ResourceInUseException to be handled, got: %v", err)
	}
	if concurrent.createCalls != 1 {
		t.Errorf("createCalls: want 1, got %d", concurrent.createCalls)
	}
}

// concurrentMockDynamoDB simulates the race where two processes run bootstrap
// simultaneously: DescribeTable returns NotFound on the first call (table not
// yet created), CreateTable returns ResourceInUseException (the other process
// beat us), and DescribeTable returns ACTIVE on the second call.
type concurrentMockDynamoDB struct {
	describeCalls int
	createCalls   int
}

func (m *concurrentMockDynamoDB) ListTables(_ context.Context, _ *dynamodb.ListTablesInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return &dynamodb.ListTablesOutput{}, nil
}

func (m *concurrentMockDynamoDB) DescribeTable(_ context.Context, _ *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	m.describeCalls++
	if m.describeCalls == 1 {
		return nil, &dbtypes.ResourceNotFoundException{}
	}
	// Second call (from waitForActiveARN): table is now ACTIVE.
	return &dynamodb.DescribeTableOutput{
		Table: &dbtypes.TableDescription{
			TableStatus: dbtypes.TableStatusActive,
			TableArn:    sdkaws.String("arn:aws:dynamodb:us-east-1:123:table/test-table"),
		},
	}, nil
}

func (m *concurrentMockDynamoDB) CreateTable(_ context.Context, _ *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	m.createCalls++
	return nil, &dbtypes.ResourceInUseException{}
}

func (m *concurrentMockDynamoDB) TagResource(_ context.Context, _ *dynamodb.TagResourceInput, _ ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
	return &dynamodb.TagResourceOutput{}, nil
}

func (m *concurrentMockDynamoDB) PutItem(_ context.Context, _ *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return &dynamodb.PutItemOutput{}, nil
}

func (m *concurrentMockDynamoDB) Scan(_ context.Context, _ *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	return &dynamodb.ScanOutput{}, nil
}

func (m *concurrentMockDynamoDB) DeleteTable(_ context.Context, _ *dynamodb.DeleteTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error) {
	return &dynamodb.DeleteTableOutput{}, nil
}

// TestEnsureLockTable_Idempotent is the core idempotency test:
// calling EnsureLockTable twice must result in exactly one CreateTable.
func TestEnsureLockTable_Idempotent(t *testing.T) {
	m := &mockDynamoDB{}

	// First call — table does not exist.
	if err := EnsureLockTable(context.Background(), m, "test-table", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if m.createCalls != 1 {
		t.Fatalf("after first call: createCalls want 1, got %d", m.createCalls)
	}

	// Second call — table is now ACTIVE (mock state updated by CreateTable).
	if err := EnsureLockTable(context.Background(), m, "test-table", nil); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if m.createCalls != 1 {
		t.Errorf("after second call: createCalls want 1 (no new create), got %d", m.createCalls)
	}
}

// TestEnsureLockTable_Schema verifies the table is created with the correct
// schema: PAY_PER_REQUEST billing and a single hash key named LockID.
func TestEnsureLockTable_Schema(t *testing.T) {
	capture := &schemaCapturingDynamoDB{}

	if err := EnsureLockTable(context.Background(), capture, "test-table", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := capture.lastCreateInput
	if in == nil {
		t.Fatal("CreateTable was not called")
	}

	if in.BillingMode != dbtypes.BillingModePayPerRequest {
		t.Errorf("BillingMode: want PAY_PER_REQUEST, got %s", in.BillingMode)
	}

	if len(in.KeySchema) != 1 {
		t.Fatalf("KeySchema: want 1 element, got %d", len(in.KeySchema))
	}
	if sdkaws.ToString(in.KeySchema[0].AttributeName) != "LockID" {
		t.Errorf("KeySchema[0].AttributeName: want LockID, got %s", sdkaws.ToString(in.KeySchema[0].AttributeName))
	}
	if in.KeySchema[0].KeyType != dbtypes.KeyTypeHash {
		t.Errorf("KeySchema[0].KeyType: want HASH, got %s", in.KeySchema[0].KeyType)
	}
}

// TestEnsureLockTable_TagsApplied verifies that when tags are provided,
// TagResource is called.
func TestEnsureLockTable_TagsApplied(t *testing.T) {
	m := &mockDynamoDB{}
	tags := map[string]string{"Project": "platform", "Layer": "bootstrap"}

	if err := EnsureLockTable(context.Background(), m, "test-table", tags); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.tagCalls != 1 {
		t.Errorf("tagCalls: want 1, got %d", m.tagCalls)
	}
}

type schemaCapturingDynamoDB struct {
	lastCreateInput *dynamodb.CreateTableInput
	tableCreated    bool
}

func (m *schemaCapturingDynamoDB) ListTables(_ context.Context, _ *dynamodb.ListTablesInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return &dynamodb.ListTablesOutput{}, nil
}

func (m *schemaCapturingDynamoDB) DescribeTable(_ context.Context, _ *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	if !m.tableCreated {
		return nil, &dbtypes.ResourceNotFoundException{}
	}
	return &dynamodb.DescribeTableOutput{
		Table: &dbtypes.TableDescription{
			TableStatus: dbtypes.TableStatusActive,
			TableArn:    sdkaws.String("arn:aws:dynamodb:us-east-1:123:table/test-table"),
		},
	}, nil
}

func (m *schemaCapturingDynamoDB) CreateTable(_ context.Context, params *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	m.lastCreateInput = params
	m.tableCreated = true
	return &dynamodb.CreateTableOutput{
		TableDescription: &dbtypes.TableDescription{
			TableStatus: dbtypes.TableStatusActive,
			TableArn:    sdkaws.String("arn:aws:dynamodb:us-east-1:123:table/test-table"),
		},
	}, nil
}

func (m *schemaCapturingDynamoDB) TagResource(_ context.Context, _ *dynamodb.TagResourceInput, _ ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
	return &dynamodb.TagResourceOutput{}, nil
}

func (m *schemaCapturingDynamoDB) PutItem(_ context.Context, _ *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return &dynamodb.PutItemOutput{}, nil
}

func (m *schemaCapturingDynamoDB) Scan(_ context.Context, _ *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	return &dynamodb.ScanOutput{}, nil
}

func (m *schemaCapturingDynamoDB) DeleteTable(_ context.Context, _ *dynamodb.DeleteTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error) {
	return &dynamodb.DeleteTableOutput{}, nil
}
