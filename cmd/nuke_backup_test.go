package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
)

// --- inspect helpers ---

type nukeBackupS3Mock struct {
	headBucketFn         func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error)
	listObjectVersionsFn func(*s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error)
	getObjectFn          func(*s3.GetObjectInput) (*s3.GetObjectOutput, error)
}

func (m *nukeBackupS3Mock) HeadBucket(_ context.Context, in *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.headBucketFn != nil {
		return m.headBucketFn(in)
	}
	return &s3.HeadBucketOutput{}, nil
}

func (m *nukeBackupS3Mock) ListObjectVersions(_ context.Context, in *s3.ListObjectVersionsInput, _ ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	if m.listObjectVersionsFn != nil {
		return m.listObjectVersionsFn(in)
	}
	return &s3.ListObjectVersionsOutput{}, nil
}

func (m *nukeBackupS3Mock) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.getObjectFn != nil {
		return m.getObjectFn(in)
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(nil))}, nil
}

// Stub methods to satisfy the full S3API interface.
func (m *nukeBackupS3Mock) ListBuckets(_ context.Context, _ *s3.ListBucketsInput, _ ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	return &s3.ListBucketsOutput{}, nil
}
func (m *nukeBackupS3Mock) CreateBucket(_ context.Context, _ *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return &s3.CreateBucketOutput{}, nil
}
func (m *nukeBackupS3Mock) PutBucketVersioning(_ context.Context, _ *s3.PutBucketVersioningInput, _ ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	return &s3.PutBucketVersioningOutput{}, nil
}
func (m *nukeBackupS3Mock) PutPublicAccessBlock(_ context.Context, _ *s3.PutPublicAccessBlockInput, _ ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error) {
	return &s3.PutPublicAccessBlockOutput{}, nil
}
func (m *nukeBackupS3Mock) PutBucketTagging(_ context.Context, _ *s3.PutBucketTaggingInput, _ ...func(*s3.Options)) (*s3.PutBucketTaggingOutput, error) {
	return &s3.PutBucketTaggingOutput{}, nil
}
func (m *nukeBackupS3Mock) DeleteObjects(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	return &s3.DeleteObjectsOutput{}, nil
}
func (m *nukeBackupS3Mock) DeleteBucket(_ context.Context, _ *s3.DeleteBucketInput, _ ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	return &s3.DeleteBucketOutput{}, nil
}

type nukeBackupDynamoMock struct {
	describeTableFn func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error)
	scanFn          func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error)
}

func (m *nukeBackupDynamoMock) DescribeTable(_ context.Context, in *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	if m.describeTableFn != nil {
		return m.describeTableFn(in)
	}
	return &dynamodb.DescribeTableOutput{}, nil
}

func (m *nukeBackupDynamoMock) Scan(_ context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	if m.scanFn != nil {
		return m.scanFn(in)
	}
	return &dynamodb.ScanOutput{}, nil
}

// Stub methods to satisfy the full DynamoDBAPI interface.
func (m *nukeBackupDynamoMock) ListTables(_ context.Context, _ *dynamodb.ListTablesInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	return &dynamodb.ListTablesOutput{}, nil
}
func (m *nukeBackupDynamoMock) CreateTable(_ context.Context, _ *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	return &dynamodb.CreateTableOutput{}, nil
}
func (m *nukeBackupDynamoMock) TagResource(_ context.Context, _ *dynamodb.TagResourceInput, _ ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
	return &dynamodb.TagResourceOutput{}, nil
}
func (m *nukeBackupDynamoMock) PutItem(_ context.Context, _ *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return &dynamodb.PutItemOutput{}, nil
}
func (m *nukeBackupDynamoMock) DeleteTable(_ context.Context, _ *dynamodb.DeleteTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error) {
	return &dynamodb.DeleteTableOutput{}, nil
}

func testNukeBackupConfig() *config.Config {
	return &config.Config{
		OrgName:     "acme",
		RootEmail:   "root@example.com",
		Region:      "us-east-1",
		StateRegion: "us-east-1",
	}
}

// --- bootstrapStateBackupPlan helpers ---

func TestBackupPlanHasData(t *testing.T) {
	empty := bootstrapStateBackupPlan{}
	if empty.hasData() {
		t.Error("empty plan should have no data")
	}
	withObjects := bootstrapStateBackupPlan{StateBucketObjects: 1}
	if !withObjects.hasData() {
		t.Error("plan with objects should have data")
	}
	withMarkers := bootstrapStateBackupPlan{DeleteMarkers: 1}
	if !withMarkers.hasData() {
		t.Error("plan with delete markers should have data")
	}
	withLock := bootstrapStateBackupPlan{LockTableItems: 1}
	if !withLock.hasData() {
		t.Error("plan with lock items should have data")
	}
	withRegistry := bootstrapStateBackupPlan{RegistryTableItems: 1}
	if !withRegistry.hasData() {
		t.Error("plan with registry items should have data")
	}
}

func TestBackupPlanSummaryLines(t *testing.T) {
	plan := bootstrapStateBackupPlan{
		StateBucket:        "bucket",
		StateBucketObjects: 3,
		DeleteMarkers:      1,
		LockTable:          "locks",
		LockTableItems:     2,
		RegistryTable:      "registry",
		RegistryTableItems: 5,
	}
	lines := plan.summaryLines()
	if len(lines) != 3 {
		t.Fatalf("expected 3 summary lines, got %d", len(lines))
	}
	for _, line := range lines {
		if line == "" {
			t.Error("summary line should not be empty")
		}
	}
}

// --- inspectBootstrapStateStoresForNuke ---

func TestInspectBootstrapStateStores_NilClients(t *testing.T) {
	cfg := testNukeBackupConfig()
	plan, err := inspectBootstrapStateStoresForNuke(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.hasData() {
		t.Error("nil clients should produce empty plan")
	}
}

func TestInspectBootstrapBucket_NotFound(t *testing.T) {
	cfg := testNukeBackupConfig()
	s3mock := &nukeBackupS3Mock{
		headBucketFn: func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
			return nil, &s3types.NotFound{}
		},
	}
	clients := &platformaws.Clients{S3: s3mock, DynamoDB: &nukeBackupDynamoMock{}}
	plan, err := inspectBootstrapStateStoresForNuke(context.Background(), cfg, clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.StateBucketObjects != 0 {
		t.Errorf("expected 0 objects for missing bucket, got %d", plan.StateBucketObjects)
	}
}

func TestInspectBootstrapBucket_CountsVersionsAndMarkers(t *testing.T) {
	cfg := testNukeBackupConfig()
	key1, vid1 := "k1", "v1"
	key2, vid2 := "k2", "v2"
	s3mock := &nukeBackupS3Mock{
		headBucketFn: func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
			return &s3.HeadBucketOutput{}, nil
		},
		listObjectVersionsFn: func(*s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error) {
			return &s3.ListObjectVersionsOutput{
				Versions:      []s3types.ObjectVersion{{Key: &key1, VersionId: &vid1}},
				DeleteMarkers: []s3types.DeleteMarkerEntry{{Key: &key2, VersionId: &vid2}},
			}, nil
		},
	}
	clients := &platformaws.Clients{S3: s3mock, DynamoDB: &nukeBackupDynamoMock{}}
	plan, err := inspectBootstrapStateStoresForNuke(context.Background(), cfg, clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.StateBucketObjects != 1 {
		t.Errorf("expected 1 version, got %d", plan.StateBucketObjects)
	}
	if plan.DeleteMarkers != 1 {
		t.Errorf("expected 1 delete marker, got %d", plan.DeleteMarkers)
	}
}

func TestInspectBootstrapBucket_ListVersionsError(t *testing.T) {
	cfg := testNukeBackupConfig()
	s3mock := &nukeBackupS3Mock{
		headBucketFn: func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
			return &s3.HeadBucketOutput{}, nil
		},
		listObjectVersionsFn: func(*s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error) {
			return nil, errors.New("list versions boom")
		},
	}
	clients := &platformaws.Clients{S3: s3mock, DynamoDB: &nukeBackupDynamoMock{}}
	_, err := inspectBootstrapStateStoresForNuke(context.Background(), cfg, clients)
	if err == nil {
		t.Error("expected error from list versions failure")
	}
}

func TestInspectBootstrapBucket_Paginates(t *testing.T) {
	cfg := testNukeBackupConfig()
	page := 0
	key1, vid1 := "k1", "v1"
	key2, vid2 := "k2", "v2"
	clients := &platformaws.Clients{S3: &nukeBackupS3Mock{
		headBucketFn: func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
			return &s3.HeadBucketOutput{}, nil
		},
		listObjectVersionsFn: func(*s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error) {
			page++
			if page == 1 {
				truncated := true
				nextKey := "next"
				nextVersion := "v-next"
				return &s3.ListObjectVersionsOutput{
					Versions:            []s3types.ObjectVersion{{Key: &key1, VersionId: &vid1}},
					IsTruncated:         &truncated,
					NextKeyMarker:       &nextKey,
					NextVersionIdMarker: &nextVersion,
				}, nil
			}
			return &s3.ListObjectVersionsOutput{DeleteMarkers: []s3types.DeleteMarkerEntry{{Key: &key2, VersionId: &vid2}}}, nil
		},
	}, DynamoDB: &nukeBackupDynamoMock{}}

	plan, err := inspectBootstrapStateStoresForNuke(context.Background(), cfg, clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.StateBucketObjects != 1 || plan.DeleteMarkers != 1 || page != 2 {
		t.Fatalf("unexpected paginated plan: %+v (pages=%d)", plan, page)
	}
}

func TestInspectBootstrapTable_NotFound(t *testing.T) {
	cfg := testNukeBackupConfig()
	dynmock := &nukeBackupDynamoMock{
		describeTableFn: func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
			return nil, &dbtypes.ResourceNotFoundException{}
		},
	}
	clients := &platformaws.Clients{S3: &nukeBackupS3Mock{
		headBucketFn: func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
			return nil, &s3types.NotFound{}
		},
	}, DynamoDB: dynmock}
	plan, err := inspectBootstrapStateStoresForNuke(context.Background(), cfg, clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.LockTableItems != 0 || plan.RegistryTableItems != 0 {
		t.Error("missing table should produce 0 items")
	}
}

func TestInspectBootstrapTable_CountsItems(t *testing.T) {
	cfg := testNukeBackupConfig()
	dynmock := &nukeBackupDynamoMock{
		describeTableFn: func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
			return &dynamodb.DescribeTableOutput{}, nil
		},
		scanFn: func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
			return &dynamodb.ScanOutput{Count: 4}, nil
		},
	}
	clients := &platformaws.Clients{S3: &nukeBackupS3Mock{
		headBucketFn: func(*s3.HeadBucketInput) (*s3.HeadBucketOutput, error) {
			return nil, &s3types.NotFound{}
		},
	}, DynamoDB: dynmock}
	plan, err := inspectBootstrapStateStoresForNuke(context.Background(), cfg, clients)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.LockTableItems != 4 {
		t.Errorf("expected 4 lock items, got %d", plan.LockTableItems)
	}
	if plan.RegistryTableItems != 4 {
		t.Errorf("expected 4 registry items, got %d", plan.RegistryTableItems)
	}
}

func TestInspectBootstrapTable_Paginates(t *testing.T) {
	startKey := map[string]dbtypes.AttributeValue{"pk": &dbtypes.AttributeValueMemberS{Value: "next"}}
	called := 0
	err := inspectBootstrapTable(context.Background(), &nukeBackupDynamoMock{
		describeTableFn: func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
			return &dynamodb.DescribeTableOutput{}, nil
		},
		scanFn: func(in *dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
			called++
			if called == 1 {
				return &dynamodb.ScanOutput{Count: 2, LastEvaluatedKey: startKey}, nil
			}
			if len(in.ExclusiveStartKey) == 0 {
				t.Fatal("expected second scan to receive LastEvaluatedKey")
			}
			return &dynamodb.ScanOutput{Count: 3}, nil
		},
	}, "locks", func(n int) {
		if n != 5 {
			t.Fatalf("count = %d, want 5", n)
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- backupBootstrapStateStoresForNuke ---

func TestBackupBootstrapStateStores_NilClients(t *testing.T) {
	cfg := testNukeBackupConfig()
	err := backupBootstrapStateStoresForNuke(context.Background(), cfg, nil, t.TempDir(), bootstrapStateBackupPlan{})
	if err != nil {
		t.Fatalf("nil clients should be a no-op, got: %v", err)
	}
}

func TestBackupBootstrapStateStores_WritesManifest(t *testing.T) {
	cfg := testNukeBackupConfig()
	dir := t.TempDir()

	plan := bootstrapStateBackupPlan{
		StateBucket:        "acme-tf-state-root",
		StateBucketObjects: 0,
		DeleteMarkers:      0,
		LockTable:          "acme-tf-locks-root",
		LockTableItems:     0,
		RegistryTable:      "acme-bootstrap-registry",
		RegistryTableItems: 0,
	}
	clients := &platformaws.Clients{
		S3:       &nukeBackupS3Mock{},
		DynamoDB: &nukeBackupDynamoMock{},
	}

	if err := backupBootstrapStateStoresForNuke(context.Background(), cfg, clients, dir, plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
	if manifest["org"] != "acme" {
		t.Errorf("manifest org = %v, want acme", manifest["org"])
	}
}

func TestBackupBootstrapStateStores_MkdirError(t *testing.T) {
	cfg := testNukeBackupConfig()
	root := t.TempDir()
	blockingPath := filepath.Join(root, "file")
	if err := os.WriteFile(blockingPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	err := backupBootstrapStateStoresForNuke(context.Background(), cfg, &platformaws.Clients{}, filepath.Join(blockingPath, "child"), bootstrapStateBackupPlan{})
	if err == nil {
		t.Fatal("expected MkdirAll error")
	}
}

func TestBackupBootstrapBucket_DownloadsVersions(t *testing.T) {
	dir := t.TempDir()
	key, vid := "mykey", "myversion"
	content := []byte("hello backup")
	s3mock := &nukeBackupS3Mock{
		listObjectVersionsFn: func(*s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error) {
			size := int64(len(content))
			return &s3.ListObjectVersionsOutput{
				Versions: []s3types.ObjectVersion{
					{Key: &key, VersionId: &vid, Size: &size},
				},
			}, nil
		},
		getObjectFn: func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(content))}, nil
		},
	}

	meta, err := backupBootstrapBucket(context.Background(), s3mock, "test-bucket", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meta) != 1 {
		t.Fatalf("expected 1 metadata entry, got %d", len(meta))
	}
	if meta[0]["key"] != key {
		t.Errorf("metadata key = %v, want %q", meta[0]["key"], key)
	}

	// Verify file was written
	filePath := filepath.Join(dir, fmt.Sprintf("object-%06d.bin", 1))
	written, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("downloaded file not found: %v", err)
	}
	if string(written) != string(content) {
		t.Errorf("file content = %q, want %q", written, content)
	}
}

func TestBackupBootstrapBucket_TrackDeleteMarkers(t *testing.T) {
	dir := t.TempDir()
	key, vid := "deleted-key", "del-vid"
	s3mock := &nukeBackupS3Mock{
		listObjectVersionsFn: func(*s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error) {
			return &s3.ListObjectVersionsOutput{
				DeleteMarkers: []s3types.DeleteMarkerEntry{{Key: &key, VersionId: &vid}},
			}, nil
		},
	}

	meta, err := backupBootstrapBucket(context.Background(), s3mock, "test-bucket", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meta) != 1 {
		t.Fatalf("expected 1 metadata entry for delete marker, got %d", len(meta))
	}
	if meta[0]["delete_marker"] != true {
		t.Errorf("expected delete_marker=true in metadata")
	}
}

func TestBackupBootstrapBucket_MkdirError(t *testing.T) {
	root := t.TempDir()
	blocking := filepath.Join(root, "file")
	if err := os.WriteFile(blocking, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	_, err := backupBootstrapBucket(context.Background(), &nukeBackupS3Mock{}, "test-bucket", filepath.Join(blocking, "child"))
	if err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestBackupBootstrapTable_WritesJSON(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dynamodb", "my-table.json")

	dynmock := &nukeBackupDynamoMock{
		scanFn: func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
			val := "hello"
			return &dynamodb.ScanOutput{
				Items: []map[string]dbtypes.AttributeValue{
					{"key": &dbtypes.AttributeValueMemberS{Value: val}},
				},
			}, nil
		},
	}

	if err := backupBootstrapTable(context.Background(), dynmock, "my-table", target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("backup file not found: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("backup file not valid JSON: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestBackupBootstrapTable_ScanError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "table.json")

	dynmock := &nukeBackupDynamoMock{
		scanFn: func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
			return nil, errors.New("scan failed")
		},
	}

	err := backupBootstrapTable(context.Background(), dynmock, "my-table", target)
	if err == nil {
		t.Error("expected error from scan failure")
	}
}

func TestBackupBootstrapTable_MkdirError(t *testing.T) {
	root := t.TempDir()
	blocking := filepath.Join(root, "file")
	if err := os.WriteFile(blocking, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	err := backupBootstrapTable(context.Background(), &nukeBackupDynamoMock{}, "table", filepath.Join(blocking, "child", "table.json"))
	if err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestDownloadBootstrapBucketVersion_GetObjectError(t *testing.T) {
	err := downloadBootstrapBucketVersion(context.Background(), &nukeBackupS3Mock{
		getObjectFn: func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			return nil, errors.New("download failed")
		},
	}, "bucket", "key", "version", filepath.Join(t.TempDir(), "out.bin"))
	if err == nil || !strings.Contains(err.Error(), "download s3://bucket/key?versionId=version") {
		t.Fatalf("expected wrapped download error, got %v", err)
	}
}

func TestWriteBootstrapJSON_CreatesFileWithCorrectPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	if err := writeBootstrapJSON(path, map[string]string{"foo": "bar"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out["foo"] != "bar" {
		t.Errorf("got %v, want bar", out["foo"])
	}
}

func TestWriteBootstrapJSON_MarshalError(t *testing.T) {
	err := writeBootstrapJSON(filepath.Join(t.TempDir(), "out.json"), map[string]any{"bad": make(chan int)})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestDefaultBootstrapBackupDirForNuke(t *testing.T) {
	dir := defaultBootstrapBackupDirForNuke("/repo/root")
	if dir == "" {
		t.Error("backup dir should not be empty")
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %q", dir)
	}
}

// --- backupS3IfNeeded / backupDynamoIfNeeded ---

func TestBackupS3IfNeeded_SkipsWhenNoData(t *testing.T) {
	dir := t.TempDir()
	manifest := map[string]any{}
	clients := &platformaws.Clients{S3: &nukeBackupS3Mock{}}
	plan := bootstrapStateBackupPlan{StateBucket: "b", StateBucketObjects: 0, DeleteMarkers: 0}
	if err := backupS3IfNeeded(context.Background(), clients, plan, dir, manifest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := manifest["s3_objects"]; ok {
		t.Error("manifest should not contain s3_objects when no data")
	}
}

func TestBackupS3IfNeeded_WritesWhenDataPresent(t *testing.T) {
	dir := t.TempDir()
	key, vid := "k", "v"
	content := []byte("data")
	s3mock := &nukeBackupS3Mock{
		listObjectVersionsFn: func(*s3.ListObjectVersionsInput) (*s3.ListObjectVersionsOutput, error) {
			size := int64(len(content))
			return &s3.ListObjectVersionsOutput{
				Versions: []s3types.ObjectVersion{{Key: &key, VersionId: &vid, Size: &size}},
			}, nil
		},
		getObjectFn: func(*s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(content))}, nil
		},
	}
	manifest := map[string]any{}
	clients := &platformaws.Clients{S3: s3mock}
	plan := bootstrapStateBackupPlan{StateBucket: "b", StateBucketObjects: 1}
	if err := backupS3IfNeeded(context.Background(), clients, plan, dir, manifest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := manifest["s3_objects"]; !ok {
		t.Error("manifest should contain s3_objects when data present")
	}
}

func TestBackupDynamoIfNeeded_SkipsWhenNoItems(t *testing.T) {
	dir := t.TempDir()
	manifest := map[string]any{}
	clients := &platformaws.Clients{DynamoDB: &nukeBackupDynamoMock{}}
	plan := bootstrapStateBackupPlan{LockTable: "lt", RegistryTable: "rt"}
	if err := backupDynamoIfNeeded(context.Background(), clients, plan, dir, manifest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := manifest["lock_table_backup"]; ok {
		t.Error("manifest should not contain lock_table_backup when 0 items")
	}
}

func TestBackupDynamoIfNeeded_WritesWhenItemsPresent(t *testing.T) {
	dir := t.TempDir()
	dynmock := &nukeBackupDynamoMock{
		scanFn: func(*dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
			val := "v"
			return &dynamodb.ScanOutput{
				Items: []map[string]dbtypes.AttributeValue{
					{"k": &dbtypes.AttributeValueMemberS{Value: val}},
				},
			}, nil
		},
	}
	manifest := map[string]any{}
	clients := &platformaws.Clients{DynamoDB: dynmock}
	plan := bootstrapStateBackupPlan{LockTable: "lt", LockTableItems: 1, RegistryTable: "rt", RegistryTableItems: 2}
	if err := backupDynamoIfNeeded(context.Background(), clients, plan, dir, manifest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := manifest["lock_table_backup"]; !ok {
		t.Error("manifest should contain lock_table_backup")
	}
	if _, ok := manifest["registry_table_backup"]; !ok {
		t.Error("manifest should contain registry_table_backup")
	}
}

func TestBackupDynamoIfNeeded_PropagatesErrors(t *testing.T) {
	t.Run("lock table backup error", func(t *testing.T) {
		dynmock := &nukeBackupDynamoMock{
			scanFn: func(in *dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
				if *in.TableName == "lt" {
					return nil, errors.New("lock scan failed")
				}
				return &dynamodb.ScanOutput{}, nil
			},
		}
		clients := &platformaws.Clients{DynamoDB: dynmock}
		err := backupDynamoIfNeeded(context.Background(), clients, bootstrapStateBackupPlan{LockTable: "lt", LockTableItems: 1, RegistryTable: "rt", RegistryTableItems: 1}, t.TempDir(), map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "scan table lt") {
			t.Fatalf("expected lock table error, got %v", err)
		}
	})

	t.Run("registry table backup error", func(t *testing.T) {
		dynmock := &nukeBackupDynamoMock{
			scanFn: func(in *dynamodb.ScanInput) (*dynamodb.ScanOutput, error) {
				if *in.TableName == "lt" {
					val := "ok"
					return &dynamodb.ScanOutput{Items: []map[string]dbtypes.AttributeValue{{"k": &dbtypes.AttributeValueMemberS{Value: val}}}}, nil
				}
				return nil, errors.New("registry scan failed")
			},
		}
		clients := &platformaws.Clients{DynamoDB: dynmock}
		err := backupDynamoIfNeeded(context.Background(), clients, bootstrapStateBackupPlan{LockTable: "lt", LockTableItems: 1, RegistryTable: "rt", RegistryTableItems: 1}, t.TempDir(), map[string]any{})
		if err == nil || !strings.Contains(err.Error(), "scan table rt") {
			t.Fatalf("expected registry table error, got %v", err)
		}
	})
}
