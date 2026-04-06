package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
)

type bootstrapNukeBackupS3API interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type bootstrapNukeBackupDynamoAPI interface {
	DescribeTable(context.Context, *dynamodb.DescribeTableInput, ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	Scan(context.Context, *dynamodb.ScanInput, ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
}

type bootstrapStateBackupPlan struct {
	StateBucket        string
	StateBucketObjects int
	DeleteMarkers      int
	LockTable          string
	LockTableItems     int
	RegistryTable      string
	RegistryTableItems int
}

func (p bootstrapStateBackupPlan) hasData() bool {
	return p.StateBucketObjects > 0 || p.DeleteMarkers > 0 || p.LockTableItems > 0 || p.RegistryTableItems > 0
}

func (p bootstrapStateBackupPlan) summaryLines() []string {
	return []string{
		fmt.Sprintf("S3 bucket %s: %d object version(s), %d delete marker(s)", p.StateBucket, p.StateBucketObjects, p.DeleteMarkers),
		fmt.Sprintf("DynamoDB table %s: %d item(s)", p.LockTable, p.LockTableItems),
		fmt.Sprintf("DynamoDB table %s: %d item(s)", p.RegistryTable, p.RegistryTableItems),
	}
}

var (
	inspectBootstrapStateStoresForNukeFn = inspectBootstrapStateStoresForNuke
	backupBootstrapStateStoresForNukeFn  = backupBootstrapStateStoresForNuke
	defaultBootstrapBackupDirForNukeFn   = defaultBootstrapBackupDirForNuke
)

func defaultBootstrapBackupDirForNuke(repoRoot string) string {
	return filepath.Join(repoRoot, ".backups", "nuke", time.Now().UTC().Format("20060102T150405Z"), "bootstrap")
}

func inspectBootstrapStateStoresForNuke(ctx context.Context, cfg *config.Config, clients *platformaws.Clients) (bootstrapStateBackupPlan, error) {
	plan := bootstrapStateBackupPlan{
		StateBucket:   cfg.StateBucketName(),
		LockTable:     cfg.LockTableName(),
		RegistryTable: cfg.RegistryTableName(),
	}
	if clients == nil {
		return plan, nil
	}

	s3Client, _ := clients.S3.(bootstrapNukeBackupS3API)
	if s3Client != nil {
		if err := inspectBootstrapBucket(ctx, s3Client, &plan); err != nil {
			return bootstrapStateBackupPlan{}, err
		}
	}

	dynamoClient, _ := clients.DynamoDB.(bootstrapNukeBackupDynamoAPI)
	if dynamoClient != nil {
		if err := inspectBootstrapTable(ctx, dynamoClient, plan.LockTable, func(n int) { plan.LockTableItems = n }); err != nil {
			return bootstrapStateBackupPlan{}, err
		}
		if err := inspectBootstrapTable(ctx, dynamoClient, plan.RegistryTable, func(n int) { plan.RegistryTableItems = n }); err != nil {
			return bootstrapStateBackupPlan{}, err
		}
	}

	return plan, nil
}

func inspectBootstrapBucket(ctx context.Context, client bootstrapNukeBackupS3API, plan *bootstrapStateBackupPlan) error {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &plan.StateBucket})
	if err != nil {
		var notFound *s3types.NotFound
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("check bucket %s: %w", plan.StateBucket, err)
	}
	var keyMarker, versionMarker *string
	for {
		out, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket:          &plan.StateBucket,
			KeyMarker:       keyMarker,
			VersionIdMarker: versionMarker,
		})
		if err != nil {
			return fmt.Errorf("list bucket versions %s: %w", plan.StateBucket, err)
		}
		plan.StateBucketObjects += len(out.Versions)
		plan.DeleteMarkers += len(out.DeleteMarkers)
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		keyMarker = out.NextKeyMarker
		versionMarker = out.NextVersionIdMarker
	}
	return nil
}

func inspectBootstrapTable(ctx context.Context, client bootstrapNukeBackupDynamoAPI, table string, setCount func(int)) error {
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: &table})
	if err != nil {
		var notFound *dbtypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("describe table %s: %w", table, err)
	}

	count := 0
	var startKey map[string]dbtypes.AttributeValue
	for {
		out, err := client.Scan(ctx, &dynamodb.ScanInput{
			TableName:         &table,
			ExclusiveStartKey: startKey,
			Select:            dbtypes.SelectCount,
		})
		if err != nil {
			return fmt.Errorf("scan table %s: %w", table, err)
		}
		count += int(out.Count)
		if len(out.LastEvaluatedKey) == 0 {
			break
		}
		startKey = out.LastEvaluatedKey
	}
	setCount(count)
	return nil
}

func backupBootstrapStateStoresForNuke(ctx context.Context, cfg *config.Config, clients *platformaws.Clients, dir string, plan bootstrapStateBackupPlan) error {
	if clients == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	manifest := map[string]any{
		"created_at":           time.Now().UTC().Format(time.RFC3339),
		"org":                  cfg.OrgName,
		"state_bucket":         plan.StateBucket,
		"state_bucket_objects": plan.StateBucketObjects,
		"state_delete_markers": plan.DeleteMarkers,
		"lock_table":           plan.LockTable,
		"lock_table_items":     plan.LockTableItems,
		"registry_table":       plan.RegistryTable,
		"registry_table_items": plan.RegistryTableItems,
	}

	if err := backupS3IfNeeded(ctx, clients, plan, dir, manifest); err != nil {
		return err
	}
	if err := backupDynamoIfNeeded(ctx, clients, plan, dir, manifest); err != nil {
		return err
	}

	return writeBootstrapJSON(filepath.Join(dir, "manifest.json"), manifest)
}

func backupS3IfNeeded(ctx context.Context, clients *platformaws.Clients, plan bootstrapStateBackupPlan, dir string, manifest map[string]any) error {
	s3Client, ok := clients.S3.(bootstrapNukeBackupS3API)
	if !ok || (plan.StateBucketObjects == 0 && plan.DeleteMarkers == 0) {
		return nil
	}
	meta, err := backupBootstrapBucket(ctx, s3Client, plan.StateBucket, filepath.Join(dir, "s3", plan.StateBucket))
	if err != nil {
		return err
	}
	manifest["s3_objects"] = meta
	return nil
}

func backupDynamoIfNeeded(ctx context.Context, clients *platformaws.Clients, plan bootstrapStateBackupPlan, dir string, manifest map[string]any) error {
	dynamoClient, ok := clients.DynamoDB.(bootstrapNukeBackupDynamoAPI)
	if !ok {
		return nil
	}
	if plan.LockTableItems > 0 {
		path := filepath.Join(dir, "dynamodb", plan.LockTable+".json")
		if err := backupBootstrapTable(ctx, dynamoClient, plan.LockTable, path); err != nil {
			return err
		}
		manifest["lock_table_backup"] = path
	}
	if plan.RegistryTableItems > 0 {
		path := filepath.Join(dir, "dynamodb", plan.RegistryTable+".json")
		if err := backupBootstrapTable(ctx, dynamoClient, plan.RegistryTable, path); err != nil {
			return err
		}
		manifest["registry_table_backup"] = path
	}
	return nil
}

func backupBootstrapBucket(ctx context.Context, client bootstrapNukeBackupS3API, bucket, dir string) ([]map[string]any, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	var keyMarker, versionMarker *string
	index := 0
	var metadata []map[string]any
	for {
		out, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket:          &bucket,
			KeyMarker:       keyMarker,
			VersionIdMarker: versionMarker,
		})
		if err != nil {
			return nil, fmt.Errorf("list bucket versions %s: %w", bucket, err)
		}
		for _, version := range out.Versions {
			index++
			name := fmt.Sprintf("object-%06d.bin", index)
			target := filepath.Join(dir, name)
			if err := downloadBootstrapBucketVersion(ctx, client, bucket, *version.Key, *version.VersionId, target); err != nil {
				return nil, err
			}
			metadata = append(metadata, map[string]any{
				"file":       name,
				"key":        *version.Key,
				"version_id": *version.VersionId,
				"size":       version.Size,
			})
		}
		for _, marker := range out.DeleteMarkers {
			metadata = append(metadata, map[string]any{
				"delete_marker": true,
				"key":           *marker.Key,
				"version_id":    *marker.VersionId,
			})
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		keyMarker = out.NextKeyMarker
		versionMarker = out.NextVersionIdMarker
	}
	return metadata, nil
}

func downloadBootstrapBucketVersion(ctx context.Context, client bootstrapNukeBackupS3API, bucket, key, versionID, target string) (retErr error) {
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:    &bucket,
		Key:       &key,
		VersionId: &versionID,
	})
	if err != nil {
		return fmt.Errorf("download s3://%s/%s?versionId=%s: %w", bucket, key, versionID, err)
	}
	defer func() { _ = out.Body.Close() }()

	//nolint:gosec // target path is generated from internal backup dir + sequential index, not external input
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && retErr == nil {
			retErr = cerr
		}
	}()
	if _, err := io.Copy(file, out.Body); err != nil {
		return err
	}
	return nil
}

func backupBootstrapTable(ctx context.Context, client bootstrapNukeBackupDynamoAPI, table, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	var items []map[string]any
	var startKey map[string]dbtypes.AttributeValue
	for {
		out, err := client.Scan(ctx, &dynamodb.ScanInput{
			TableName:         &table,
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return fmt.Errorf("scan table %s: %w", table, err)
		}
		for _, raw := range out.Items {
			var decoded map[string]any
			if err := attributevalue.UnmarshalMap(raw, &decoded); err != nil {
				return fmt.Errorf("decode table %s item: %w", table, err)
			}
			items = append(items, decoded)
		}
		if len(out.LastEvaluatedKey) == 0 {
			break
		}
		startKey = out.LastEvaluatedKey
	}
	return writeBootstrapJSON(target, items)
}

func writeBootstrapJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}
