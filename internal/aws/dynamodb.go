package aws

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// tableActivePollInterval is the sleep between DescribeTable retries in
// waitForActive. Exported as a package variable so tests can set it to zero
// without changing production behaviour.
var tableActivePollInterval = 3 * time.Second

// EnsureLockTable creates the DynamoDB lock table if it does not exist.
//
// Schema:   PK=LockID (S), billing=PAY_PER_REQUEST
// This schema is the standard Terraform state-locking contract.
//
// tags is applied when non-empty; pass nil to skip tagging.
// A tagging failure is always fatal.
func EnsureLockTable(ctx context.Context, client DynamoDBAPI, name string, tags map[string]string) error {
	return ensureTable(ctx, client, name, &dynamodb.CreateTableInput{
		TableName: sdkaws.String(name),
		AttributeDefinitions: []dbtypes.AttributeDefinition{
			{
				AttributeName: sdkaws.String("LockID"),
				AttributeType: dbtypes.ScalarAttributeTypeS,
			},
		},
		KeySchema: []dbtypes.KeySchemaElement{
			{
				AttributeName: sdkaws.String("LockID"),
				KeyType:       dbtypes.KeyTypeHash,
			},
		},
		BillingMode: dbtypes.BillingModePayPerRequest,
	}, tags)
}

// ensureTable creates the named DynamoDB table using input if it does not
// exist, waits for ACTIVE, and applies tags when provided.
//
// Shared by EnsureLockTable and EnsureRegistryTable so both get consistent
// idempotency semantics: DescribeTable→ACTIVE skips create; ResourceInUseException
// on concurrent create is treated as success; wait loop is always applied.
func ensureTable(ctx context.Context, client DynamoDBAPI, name string, input *dynamodb.CreateTableInput, tags map[string]string) error {
	tableARN, err := ensureTableActiveARN(ctx, client, name, input)
	if err != nil {
		return err
	}
	if len(tags) > 0 {
		if err := tagDynamoDBTable(ctx, client, tableARN, tags); err != nil {
			return fmt.Errorf("tagging table %s: %w", name, err)
		}
	}
	return nil
}

// ensureTableActiveARN ensures the table exists and is ACTIVE, then returns
// its ARN. It avoids an extra DescribeTable call by capturing the ARN from
// the create output or the initial describe output.
func ensureTableActiveARN(ctx context.Context, client DynamoDBAPI, name string, input *dynamodb.CreateTableInput) (string, error) {
	out, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: sdkaws.String(name),
	})
	if err == nil {
		if out.Table.TableStatus == dbtypes.TableStatusActive {
			return sdkaws.ToString(out.Table.TableArn), nil
		}
		// Transitional state — wait for ACTIVE.
		return waitForActiveARN(ctx, client, name)
	}

	var notFound *dbtypes.ResourceNotFoundException
	if !errors.As(err, &notFound) {
		return "", fmt.Errorf("checking table %s: %w", name, err)
	}

	// Table does not exist — create it.
	_, createErr := client.CreateTable(ctx, input)
	if createErr != nil {
		// ResourceInUseException means a concurrent run created the table.
		// Fall through to waitForActiveARN so we confirm it reaches ACTIVE.
		var inUse *dbtypes.ResourceInUseException
		if !errors.As(createErr, &inUse) {
			return "", fmt.Errorf("creating table %s: %w", name, createErr)
		}
	}

	return waitForActiveARN(ctx, client, name)
}

// waitForActiveARN polls DescribeTable until the table reaches ACTIVE or the
// context deadline is exceeded, then returns the table ARN.
func waitForActiveARN(ctx context.Context, client DynamoDBAPI, name string) (string, error) {
	pollCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	for {
		out, err := client.DescribeTable(pollCtx, &dynamodb.DescribeTableInput{
			TableName: sdkaws.String(name),
		})
		if err != nil {
			return "", fmt.Errorf("polling table %s: %w", name, err)
		}
		if out.Table.TableStatus == dbtypes.TableStatusActive {
			return sdkaws.ToString(out.Table.TableArn), nil
		}

		select {
		case <-pollCtx.Done():
			return "", fmt.Errorf("timed out waiting for table %s to become active", name)
		case <-time.After(tableActivePollInterval):
		}
	}
}

// tagDynamoDBTable applies the given tags to the table using its ARN.
// TagResource is idempotent — re-applying the same tags is safe.
func tagDynamoDBTable(ctx context.Context, client DynamoDBAPI, tableARN string, tags map[string]string) error {
	dbTags := make([]dbtypes.Tag, 0, len(tags))
	for k, v := range tags {
		dbTags = append(dbTags, dbtypes.Tag{
			Key:   sdkaws.String(k),
			Value: sdkaws.String(v),
		})
	}
	_, err := client.TagResource(ctx, &dynamodb.TagResourceInput{
		ResourceArn: sdkaws.String(tableARN),
		Tags:        dbTags,
	})
	if err != nil {
		return fmt.Errorf("tagging DynamoDB resource %s: %w", tableARN, err)
	}
	return nil
}
