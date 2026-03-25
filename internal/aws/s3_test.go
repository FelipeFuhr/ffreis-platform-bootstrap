package aws

import (
	"context"
	"errors"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// errTaggingFailed is a sentinel used by tagging failure tests.
var errTaggingFailed = errors.New("tagging failed")

// mockS3 is a stateful stand-in for S3API. bucketExists transitions to true
// after a successful CreateBucket, mirroring real AWS behaviour.
type mockS3 struct {
	bucketExists     bool
	createErr        error
	createCalls      int
	versioningCalls  int
	publicBlockCalls int
	taggingCalls     int
	taggingErr       error
}

func (m *mockS3) ListBuckets(_ context.Context, _ *s3.ListBucketsInput, _ ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	return &s3.ListBucketsOutput{}, nil
}

func (m *mockS3) HeadBucket(_ context.Context, _ *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if m.bucketExists {
		return &s3.HeadBucketOutput{}, nil
	}
	return nil, &s3types.NotFound{}
}

func (m *mockS3) CreateBucket(_ context.Context, _ *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	m.createCalls++
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.bucketExists = true
	return &s3.CreateBucketOutput{}, nil
}

func (m *mockS3) PutBucketVersioning(_ context.Context, _ *s3.PutBucketVersioningInput, _ ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	m.versioningCalls++
	return &s3.PutBucketVersioningOutput{}, nil
}

func (m *mockS3) PutPublicAccessBlock(_ context.Context, _ *s3.PutPublicAccessBlockInput, _ ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error) {
	m.publicBlockCalls++
	return &s3.PutPublicAccessBlockOutput{}, nil
}

func (m *mockS3) PutBucketTagging(_ context.Context, _ *s3.PutBucketTaggingInput, _ ...func(*s3.Options)) (*s3.PutBucketTaggingOutput, error) {
	m.taggingCalls++
	return &s3.PutBucketTaggingOutput{}, m.taggingErr
}

func (m *mockS3) ListObjectVersions(_ context.Context, _ *s3.ListObjectVersionsInput, _ ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	return &s3.ListObjectVersionsOutput{}, nil
}

func (m *mockS3) DeleteObjects(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	return &s3.DeleteObjectsOutput{}, nil
}

func (m *mockS3) DeleteBucket(_ context.Context, _ *s3.DeleteBucketInput, _ ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	m.bucketExists = false
	return &s3.DeleteBucketOutput{}, nil
}

// TestEnsureStateBucket_Create verifies that when the bucket does not exist,
// CreateBucket is called exactly once and configuration is applied.
func TestEnsureStateBucket_Create(t *testing.T) {
	m := &mockS3{}

	if err := EnsureStateBucket(context.Background(), m, "test-bucket", "us-east-1", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.createCalls != 1 {
		t.Errorf("createCalls: want 1, got %d", m.createCalls)
	}
	if m.versioningCalls != 1 {
		t.Errorf("versioningCalls: want 1, got %d", m.versioningCalls)
	}
	if m.publicBlockCalls != 1 {
		t.Errorf("publicBlockCalls: want 1, got %d", m.publicBlockCalls)
	}
	if m.taggingCalls != 0 {
		t.Errorf("taggingCalls: want 0 (nil tags), got %d", m.taggingCalls)
	}
}

// TestEnsureStateBucket_AlreadyExists verifies that when HeadBucket succeeds,
// CreateBucket is never called and configuration is still applied.
func TestEnsureStateBucket_AlreadyExists(t *testing.T) {
	m := &mockS3{bucketExists: true}

	if err := EnsureStateBucket(context.Background(), m, "test-bucket", "us-east-1", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.createCalls != 0 {
		t.Errorf("createCalls: want 0 (bucket already existed), got %d", m.createCalls)
	}
	if m.versioningCalls != 1 {
		t.Errorf("versioningCalls: want 1, got %d", m.versioningCalls)
	}
	if m.publicBlockCalls != 1 {
		t.Errorf("publicBlockCalls: want 1, got %d", m.publicBlockCalls)
	}
}

// TestEnsureStateBucket_BucketAlreadyOwnedByYou verifies that a concurrent
// create (BucketAlreadyOwnedByYou) is treated as success.
func TestEnsureStateBucket_BucketAlreadyOwnedByYou(t *testing.T) {
	m := &mockS3{
		createErr: &s3types.BucketAlreadyOwnedByYou{},
	}

	if err := EnsureStateBucket(context.Background(), m, "test-bucket", "us-east-1", nil); err != nil {
		t.Fatalf("expected BucketAlreadyOwnedByYou to be treated as success, got: %v", err)
	}

	if m.createCalls != 1 {
		t.Errorf("createCalls: want 1, got %d", m.createCalls)
	}
}

// TestEnsureStateBucket_Idempotent is the core idempotency test:
// calling EnsureStateBucket twice must result in exactly one CreateBucket.
func TestEnsureStateBucket_Idempotent(t *testing.T) {
	m := &mockS3{}

	// First call — bucket does not exist yet.
	if err := EnsureStateBucket(context.Background(), m, "test-bucket", "us-east-1", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if m.createCalls != 1 {
		t.Fatalf("after first call: createCalls want 1, got %d", m.createCalls)
	}

	// Second call — bucket now exists (mock state updated by CreateBucket).
	if err := EnsureStateBucket(context.Background(), m, "test-bucket", "us-east-1", nil); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if m.createCalls != 1 {
		t.Errorf("after second call: createCalls want 1 (no new create), got %d", m.createCalls)
	}
	// Configuration is re-applied on every call.
	if m.versioningCalls != 2 {
		t.Errorf("versioningCalls: want 2 (applied each run), got %d", m.versioningCalls)
	}
}

// TestEnsureStateBucket_LocationConstraint verifies that non-us-east-1
// regions include a CreateBucketConfiguration in the CreateBucket call.
func TestEnsureStateBucket_LocationConstraint(t *testing.T) {
	var capturedInput *s3.CreateBucketInput

	m := &mockS3{}
	_ = m

	// Wrap the mock to capture the input.
	capture := &capturingS3{mockS3: m}

	if err := EnsureStateBucket(context.Background(), capture, "test-bucket", "eu-west-1", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	capturedInput = capture.lastCreateInput
	if capturedInput == nil {
		t.Fatal("CreateBucket was not called")
	}
	if capturedInput.CreateBucketConfiguration == nil {
		t.Fatal("CreateBucketConfiguration must be set for non-us-east-1 regions")
	}
	want := s3types.BucketLocationConstraint("eu-west-1")
	if capturedInput.CreateBucketConfiguration.LocationConstraint != want {
		t.Errorf("LocationConstraint: want %s, got %s",
			want, capturedInput.CreateBucketConfiguration.LocationConstraint)
	}
}

// TestEnsureStateBucket_TagsApplied verifies that when tags are provided,
// PutBucketTagging is called.
func TestEnsureStateBucket_TagsApplied(t *testing.T) {
	m := &mockS3{}
	tags := map[string]string{"Project": "platform", "Layer": "bootstrap"}

	if err := EnsureStateBucket(context.Background(), m, "test-bucket", "us-east-1", tags); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.taggingCalls != 1 {
		t.Errorf("taggingCalls: want 1, got %d", m.taggingCalls)
	}
}

// TestEnsureStateBucket_TaggingFailureFatal verifies that a tagging failure
// is propagated as an error (not swallowed).
func TestEnsureStateBucket_TaggingFailureFatal(t *testing.T) {
	m := &mockS3{taggingErr: errTaggingFailed}
	tags := map[string]string{"Project": "platform"}

	if err := EnsureStateBucket(context.Background(), m, "test-bucket", "us-east-1", tags); err == nil {
		t.Fatal("expected tagging error to be returned, got nil")
	}
}

// capturingS3 wraps mockS3 and records the last CreateBucket input.
type capturingS3 struct {
	*mockS3
	lastCreateInput *s3.CreateBucketInput
}

func (c *capturingS3) CreateBucket(_ context.Context, params *s3.CreateBucketInput, _ ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	c.lastCreateInput = params
	c.mockS3.createCalls++
	c.mockS3.bucketExists = true
	return &s3.CreateBucketOutput{
		Location: sdkaws.String("/" + sdkaws.ToString(params.Bucket)),
	}, nil
}
