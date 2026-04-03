package aws

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// registryMockDynamoDB extends mockDynamoDB with PutItem idempotency behaviour
// and Scan returning stored items.
type registryMockDynamoDB struct {
	// Embed standard mock for table operations.
	mockDynamoDB

	// items is the in-memory store indexed by "PK/SK".
	items         map[string]map[string]dbtypes.AttributeValue
	putCalls      int
	condFailOnDup bool // if true, second PutItem with same key fails with ConditionalCheckFailedException
}

func newRegistryMock() *registryMockDynamoDB {
	return &registryMockDynamoDB{
		mockDynamoDB: mockDynamoDB{tableStatus: dbtypes.TableStatusActive},
		items:        make(map[string]map[string]dbtypes.AttributeValue),
	}
}

func (m *registryMockDynamoDB) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	m.putCalls++

	pkAttr := params.Item["PK"]
	skAttr := params.Item["SK"]
	pk := pkAttr.(*dbtypes.AttributeValueMemberS).Value
	sk := skAttr.(*dbtypes.AttributeValueMemberS).Value
	key := pk + "/" + sk

	if m.condFailOnDup {
		if _, exists := m.items[key]; exists {
			return nil, &dbtypes.ConditionalCheckFailedException{}
		}
	}

	m.items[key] = params.Item
	return &dynamodb.PutItemOutput{}, nil
}

func (m *registryMockDynamoDB) Scan(_ context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	items := make([]map[string]dbtypes.AttributeValue, 0, len(m.items))

	// Apply simple PK equality filter when present (mirrors FetchConfig usage).
	var filterPK string
	if in.FilterExpression != nil {
		if v, ok := in.ExpressionAttributeValues[":pk"]; ok {
			filterPK = v.(*dbtypes.AttributeValueMemberS).Value
		}
	}

	for _, item := range m.items {
		if filterPK != "" {
			pk := item["PK"].(*dbtypes.AttributeValueMemberS).Value
			if pk != filterPK {
				continue
			}
		}
		items = append(items, item)
	}
	return &dynamodb.ScanOutput{Items: items}, nil
}

type scanRegistryMock struct {
	registryMockDynamoDB
	scanErr error
}

func (m *scanRegistryMock) Scan(ctx context.Context, in *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	if m.scanErr != nil {
		return nil, m.scanErr
	}
	return m.registryMockDynamoDB.Scan(ctx, in, optFns...)
}

// TestEnsureRegistryTable_Schema verifies the registry table is created with
// the correct composite key schema (PK HASH, SK RANGE).
func TestEnsureRegistryTable_Schema(t *testing.T) {
	capture := &schemaCapturingDynamoDB{}

	if err := EnsureRegistryTable(context.Background(), capture, testRegistryTable, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in := capture.lastCreateInput
	if in == nil {
		t.Fatal("CreateTable was not called")
	}

	if in.BillingMode != dbtypes.BillingModePayPerRequest {
		t.Errorf("BillingMode: want PAY_PER_REQUEST, got %s", in.BillingMode)
	}

	if len(in.KeySchema) != 2 {
		t.Fatalf("KeySchema: want 2 elements (PK+SK), got %d", len(in.KeySchema))
	}

	keys := map[dbtypes.KeyType]string{}
	for _, k := range in.KeySchema {
		keys[k.KeyType] = sdkaws.ToString(k.AttributeName)
	}
	if keys[dbtypes.KeyTypeHash] != "PK" {
		t.Errorf("HASH key: want PK, got %s", keys[dbtypes.KeyTypeHash])
	}
	if keys[dbtypes.KeyTypeRange] != "SK" {
		t.Errorf("RANGE key: want SK, got %s", keys[dbtypes.KeyTypeRange])
	}
}

// TestRegisterResource_WritesRecord verifies that RegisterResource writes
// a record to the table and sets the expected fields.
func TestRegisterResource_WritesRecord(t *testing.T) {
	m := newRegistryMock()

	rec, err := NewRegistryRecord("S3Bucket", testStateBucket, "arn:aws:iam::123:root", map[string]string{"Project": "platform"})
	if err != nil {
		t.Fatalf("NewRegistryRecord: %v", err)
	}

	if err := RegisterResource(context.Background(), m, testRegistryTable, rec); err != nil {
		t.Fatalf("RegisterResource: %v", err)
	}

	if m.putCalls != 1 {
		t.Errorf("putCalls: want 1, got %d", m.putCalls)
	}
}

// TestRegisterResource_Idempotent verifies that a second RegisterResource call
// with the same PK+SK is treated as success (ConditionalCheckFailedException
// is swallowed).
func TestRegisterResource_Idempotent(t *testing.T) {
	m := newRegistryMock()
	m.condFailOnDup = true

	rec, _ := NewRegistryRecord("S3Bucket", testStateBucket, "arn:aws:iam::123:root", nil)

	if err := RegisterResource(context.Background(), m, testRegistryTable, rec); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := RegisterResource(context.Background(), m, testRegistryTable, rec); err != nil {
		t.Fatalf("second call (should be idempotent): %v", err)
	}

	if m.putCalls != 2 {
		t.Errorf("putCalls: want 2 (one per call), got %d", m.putCalls)
	}
	// Only one record in store — second write was rejected by condition.
	if len(m.items) != 1 {
		t.Errorf("items in store: want 1 (no duplicate), got %d", len(m.items))
	}
}

// TestNewRegistryRecord_Fields verifies all fields are populated correctly.
func TestNewRegistryRecord_Fields(t *testing.T) {
	before := time.Now().UTC()
	tags := map[string]string{"Project": "platform"}
	rec, err := NewRegistryRecord("IAMRole", "platform-admin", testCallerRoleARN, tags)
	after := time.Now().UTC()

	if err != nil {
		t.Fatalf("NewRegistryRecord: %v", err)
	}

	if rec.PK != "RESOURCE#IAMRole" {
		t.Errorf("PK: want RESOURCE#IAMRole, got %s", rec.PK)
	}
	if rec.SK != "platform-admin" {
		t.Errorf("SK: want platform-admin, got %s", rec.SK)
	}
	if rec.ResourceType != "IAMRole" {
		t.Errorf("ResourceType: want IAMRole, got %s", rec.ResourceType)
	}
	if rec.CreatedBy != testCallerRoleARN {
		t.Errorf("CreatedBy: want arn, got %s", rec.CreatedBy)
	}
	if rec.CreatedAt.Before(before) || rec.CreatedAt.After(after) {
		t.Errorf("CreatedAt %s outside expected range", rec.CreatedAt)
	}
	if rec.Tags == "" {
		t.Error("Tags must not be empty when provided")
	}
}

// TestWriteConfig_WritesRecord verifies that WriteConfig stores a ConfigRecord
// and that it can be retrieved via FetchConfig.
func TestWriteConfig_WritesRecord(t *testing.T) {
	m := newRegistryMock()
	ctx := context.Background()

	err := WriteConfig(ctx, m, testRegistryTable, "account", "development",
		testCallerRoleARN, map[string]string{"email": testDevEmail})
	if err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	records, err := FetchConfig(ctx, m, testRegistryTable, "account")
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records: want 1, got %d", len(records))
	}

	rec := records[0]
	if rec.PK != "CONFIG#account" {
		t.Errorf("PK: want CONFIG#account, got %s", rec.PK)
	}
	if rec.SK != "development" {
		t.Errorf("SK: want development, got %s", rec.SK)
	}
	if rec.Data["email"] != testDevEmail {
		t.Errorf("Data[email]: want dev@example.com, got %s", rec.Data["email"])
	}
}

// TestWriteConfig_Overwrites verifies that WriteConfig is mutable —
// a second write with the same key replaces the first.
func TestWriteConfig_Overwrites(t *testing.T) {
	m := newRegistryMock()
	ctx := context.Background()

	_ = WriteConfig(ctx, m, testRegistryTable, "account", "development", "actor", map[string]string{"email": "old@example.com"})
	_ = WriteConfig(ctx, m, testRegistryTable, "account", "development", "actor", map[string]string{"email": "new@example.com"})

	records, _ := FetchConfig(ctx, m, testRegistryTable, "account")
	if len(records) != 1 {
		t.Fatalf("records: want 1, got %d (duplicate written)", len(records))
	}
	if records[0].Data["email"] != "new@example.com" {
		t.Errorf("email: want new@example.com, got %s", records[0].Data["email"])
	}
}

// TestFetchConfig_FiltersType verifies that FetchConfig only returns records
// matching the requested configType, not other config or resource records.
func TestFetchConfig_FiltersType(t *testing.T) {
	m := newRegistryMock()
	ctx := context.Background()

	_ = WriteConfig(ctx, m, testRegistryTable, "account", "development", "actor", map[string]string{"email": testDevEmail})
	_ = WriteConfig(ctx, m, testRegistryTable, "account", "staging", "actor", map[string]string{"email": "staging@example.com"})

	// Also write a resource record that must not appear in account fetch.
	rec, _ := NewRegistryRecord("S3Bucket", testStateBucket, "actor", nil)
	_ = RegisterResource(ctx, m, testRegistryTable, rec)

	records, err := FetchConfig(ctx, m, testRegistryTable, "account")
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("records: want 2 accounts, got %d", len(records))
	}
	for _, r := range records {
		if r.PK != "CONFIG#account" {
			t.Errorf("unexpected PK in result: %s", r.PK)
		}
	}
}

// TestScanRegistry_ReturnsList verifies that ScanRegistry returns all
// records written to the table.
func TestScanRegistry_ReturnsList(t *testing.T) {
	m := newRegistryMock()

	rec1, _ := NewRegistryRecord("S3Bucket", "bucket-1", "actor", nil)
	rec2, _ := NewRegistryRecord("IAMRole", "role-1", "actor", nil)

	if err := RegisterResource(context.Background(), m, testRegistryTable, rec1); err != nil {
		t.Fatalf("register rec1: %v", err)
	}
	if err := RegisterResource(context.Background(), m, testRegistryTable, rec2); err != nil {
		t.Fatalf("register rec2: %v", err)
	}

	records, err := ScanRegistry(context.Background(), m, testRegistryTable)
	if err != nil {
		t.Fatalf("ScanRegistry: %v", err)
	}

	if len(records) != 2 {
		t.Errorf("records: want 2, got %d", len(records))
	}
}

func TestScanRegistry_MissingTableReturnsEmpty(t *testing.T) {
	t.Parallel()

	m := &scanRegistryMock{scanErr: &dbtypes.ResourceNotFoundException{}}

	records, err := ScanRegistry(context.Background(), m, testRegistryTable)
	if err != nil {
		t.Fatalf("ScanRegistry: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records: want 0 for missing table, got %d", len(records))
	}
}

func TestScanRegistry_UnexpectedError(t *testing.T) {
	t.Parallel()

	m := &scanRegistryMock{scanErr: errors.New("boom")}

	_, err := ScanRegistry(context.Background(), m, testRegistryTable)
	if err == nil {
		t.Fatal("expected ScanRegistry to fail")
	}
	if err.Error() != "scanning registry table "+testRegistryTable+": boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanRegistry_UnmarshalError(t *testing.T) {
	t.Parallel()

	m := newRegistryMock()
	m.items["RESOURCE#S3Bucket/bad"] = map[string]dbtypes.AttributeValue{
		"PK":            &dbtypes.AttributeValueMemberS{Value: "RESOURCE#S3Bucket"},
		"SK":            &dbtypes.AttributeValueMemberS{Value: "bad"},
		"resource_type": &dbtypes.AttributeValueMemberS{Value: "S3Bucket"},
		"resource_name": &dbtypes.AttributeValueMemberS{Value: "bad"},
		"created_at":    &dbtypes.AttributeValueMemberS{Value: "not-a-timestamp"},
		"created_by":    &dbtypes.AttributeValueMemberS{Value: "actor"},
		"tags":          &dbtypes.AttributeValueMemberS{Value: "{}"},
	}

	_, err := ScanRegistry(context.Background(), m, testRegistryTable)
	if err == nil {
		t.Fatal("expected ScanRegistry to fail on invalid item")
	}
	if !strings.Contains(err.Error(), "unmarshalling registry record") {
		t.Fatalf("unexpected error: %v", err)
	}
}
