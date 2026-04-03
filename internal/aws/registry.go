package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// RegistryRecord is a single entry in the bootstrap registry table.
// Every platform-managed resource has one record.
type RegistryRecord struct {
	// PK is "RESOURCE#<ResourceType>", e.g. "RESOURCE#S3Bucket".
	PK string `dynamodbav:"PK"`

	// SK is the resource name, e.g. "ffreis-tf-state-root".
	SK string `dynamodbav:"SK"`

	// ResourceType is the AWS resource category, e.g. "S3Bucket".
	// Redundant with PK but stored separately for query convenience.
	ResourceType string `dynamodbav:"resource_type"`

	// ResourceName is the resource identifier within its type.
	ResourceName string `dynamodbav:"resource_name"`

	// CreatedAt is the UTC timestamp when the record was first written.
	CreatedAt time.Time `dynamodbav:"created_at"`

	// CreatedBy is the IAM principal ARN that ran the bootstrap.
	CreatedBy string `dynamodbav:"created_by"`

	// Tags is the JSON-encoded map of resource tags applied at creation.
	Tags string `dynamodbav:"tags"`
}

// NewRegistryRecord constructs a RegistryRecord ready for PutItem.
func NewRegistryRecord(resourceType, resourceName, callerARN string, tags map[string]string) (RegistryRecord, error) {
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return RegistryRecord{}, fmt.Errorf("marshalling tags for registry: %w", err)
	}
	return RegistryRecord{
		PK:           "RESOURCE#" + resourceType,
		SK:           resourceName,
		ResourceType: resourceType,
		ResourceName: resourceName,
		CreatedAt:    time.Now().UTC(),
		CreatedBy:    callerARN,
		Tags:         string(tagsJSON),
	}, nil
}

// EnsureRegistryTable creates the bootstrap registry DynamoDB table if it does
// not exist. Schema: PK=PK (S) HASH, SK=SK (S) RANGE, PAY_PER_REQUEST.
//
// tags is applied when non-empty; pass nil to skip tagging.
func EnsureRegistryTable(ctx context.Context, client DynamoDBAPI, name string, tags map[string]string) error {
	return ensureTable(ctx, client, name, &dynamodb.CreateTableInput{
		TableName: sdkaws.String(name),
		AttributeDefinitions: []dbtypes.AttributeDefinition{
			{
				AttributeName: sdkaws.String("PK"),
				AttributeType: dbtypes.ScalarAttributeTypeS,
			},
			{
				AttributeName: sdkaws.String("SK"),
				AttributeType: dbtypes.ScalarAttributeTypeS,
			},
		},
		KeySchema: []dbtypes.KeySchemaElement{
			{
				AttributeName: sdkaws.String("PK"),
				KeyType:       dbtypes.KeyTypeHash,
			},
			{
				AttributeName: sdkaws.String("SK"),
				KeyType:       dbtypes.KeyTypeRange,
			},
		},
		BillingMode: dbtypes.BillingModePayPerRequest,
	}, tags)
}

// RegisterResource writes a RegistryRecord to the registry table using a
// conditional expression so that an existing record is never overwritten.
//
// Idempotency: if an item with the same PK+SK already exists,
// ConditionalCheckFailedException is returned by DynamoDB — this is treated
// as success, meaning the resource was already registered on a previous run.
func RegisterResource(ctx context.Context, client DynamoDBAPI, tableName string, rec RegistryRecord) error {
	item, err := attributevalue.MarshalMap(rec)
	if err != nil {
		return fmt.Errorf("marshalling registry record for %s/%s: %w", rec.ResourceType, rec.ResourceName, err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           sdkaws.String(tableName),
		Item:                item,
		ConditionExpression: sdkaws.String("attribute_not_exists(PK)"),
	})
	if err != nil {
		// ConditionalCheckFailedException means the item already exists.
		// This is the expected idempotency outcome on re-runs.
		var condFailed *dbtypes.ConditionalCheckFailedException
		if errors.As(err, &condFailed) {
			return nil
		}
		return fmt.Errorf("registering resource %s/%s: %w", rec.ResourceType, rec.ResourceName, err)
	}
	return nil
}

// ConfigRecord is a configuration entry in the bootstrap registry table.
// Unlike RegistryRecord (which tracks created AWS resources), ConfigRecord
// stores declarative platform configuration such as account definitions.
// PK is "CONFIG#<ConfigType>", SK is the config name (e.g. account name).
type ConfigRecord struct {
	PK         string            `dynamodbav:"PK"`
	SK         string            `dynamodbav:"SK"`
	ConfigType string            `dynamodbav:"config_type"`
	ConfigName string            `dynamodbav:"config_name"`
	UpdatedAt  time.Time         `dynamodbav:"updated_at"`
	UpdatedBy  string            `dynamodbav:"updated_by"`
	Data       map[string]string `dynamodbav:"data"`
}

// WriteConfig writes a ConfigRecord to the registry table, unconditionally
// overwriting any previous value. Config records are mutable — unlike
// resource records, they are expected to be updated (e.g. email changes).
func WriteConfig(ctx context.Context, client DynamoDBAPI, tableName, configType, configName, callerARN string, data map[string]string) error {
	rec := ConfigRecord{
		PK:         "CONFIG#" + configType,
		SK:         configName,
		ConfigType: configType,
		ConfigName: configName,
		UpdatedAt:  time.Now().UTC(),
		UpdatedBy:  callerARN,
		Data:       data,
	}
	item, err := attributevalue.MarshalMap(rec)
	if err != nil {
		return fmt.Errorf("marshalling config record %s/%s: %w", configType, configName, err)
	}
	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: sdkaws.String(tableName),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("writing config record %s/%s: %w", configType, configName, err)
	}
	return nil
}

// FetchConfig returns all ConfigRecord entries for a given configType by
// scanning the registry table with a PK filter.
func FetchConfig(ctx context.Context, client DynamoDBAPI, tableName, configType string) ([]ConfigRecord, error) {
	pk := "CONFIG#" + configType
	var records []ConfigRecord
	var lastKey map[string]dbtypes.AttributeValue

	for {
		input := &dynamodb.ScanInput{
			TableName:        sdkaws.String(tableName),
			FilterExpression: sdkaws.String("PK = :pk"),
			ExpressionAttributeValues: map[string]dbtypes.AttributeValue{
				":pk": &dbtypes.AttributeValueMemberS{Value: pk},
			},
		}
		if lastKey != nil {
			input.ExclusiveStartKey = lastKey
		}

		out, err := client.Scan(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("fetching config %s from %s: %w", configType, tableName, err)
		}

		for _, item := range out.Items {
			var rec ConfigRecord
			if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
				return nil, fmt.Errorf("unmarshalling config record: %w", err)
			}
			records = append(records, rec)
		}

		if out.LastEvaluatedKey == nil {
			break
		}
		lastKey = out.LastEvaluatedKey
	}

	return records, nil
}

// ScanRegistry returns all RegistryRecord entries from the registry table.
// This is used by the audit command to list all managed resources.
//
// If the registry table does not exist (e.g. after a nuke or before the first
// init), ScanRegistry returns an empty slice and no error. The audit command
// will then surface all expected resources as "unmanaged" based purely on what
// exists in AWS, allowing a useful audit even in a fully cleaned-up state.
func ScanRegistry(ctx context.Context, client DynamoDBAPI, tableName string) ([]RegistryRecord, error) {
	var records []RegistryRecord
	var lastKey map[string]dbtypes.AttributeValue

	for {
		input := &dynamodb.ScanInput{
			TableName: sdkaws.String(tableName),
		}
		if lastKey != nil {
			input.ExclusiveStartKey = lastKey
		}

		out, err := client.Scan(ctx, input)
		if err != nil {
			var notFound *dbtypes.ResourceNotFoundException
			if errors.As(err, &notFound) {
				return nil, nil // table gone — treat as empty registry
			}
			return nil, fmt.Errorf("scanning registry table %s: %w", tableName, err)
		}

		for _, item := range out.Items {
			var rec RegistryRecord
			if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
				return nil, fmt.Errorf("unmarshalling registry record: %w", err)
			}
			records = append(records, rec)
		}

		if out.LastEvaluatedKey == nil {
			break
		}
		lastKey = out.LastEvaluatedKey
	}

	return records, nil
}
