package aws

import (
	"context"
	"errors"
	"fmt"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// EnsureStateBucket creates the S3 bucket if it does not exist, then
// enforces versioning, public-access block, and resource tags on every run.
//
// Idempotency:
//   - HeadBucket succeeds  → bucket exists; skip CreateBucket.
//   - CreateBucket returns BucketAlreadyOwnedByYou → treat as success.
//   - PutBucketVersioning and PutPublicAccessBlock are PUT operations;
//     re-applying them when already configured has no effect.
//   - PutBucketTagging overwrites the tag set; re-applying identical tags is safe.
//
// tags is applied when non-empty; pass nil to skip tagging (e.g. in tests).
// A tagging failure is always fatal — tags are mandatory on all platform resources.
func EnsureStateBucket(ctx context.Context, client S3API, name, region string, tags map[string]string) error {
	if err := ensureBucketExists(ctx, client, name, region); err != nil {
		return err
	}
	if err := enableVersioning(ctx, client, name); err != nil {
		return fmt.Errorf("enabling versioning on %s: %w", name, err)
	}
	if err := blockPublicAccess(ctx, client, name); err != nil {
		return fmt.Errorf("blocking public access on %s: %w", name, err)
	}
	if len(tags) > 0 {
		if err := tagBucket(ctx, client, name, tags); err != nil {
			return err
		}
	}
	return nil
}

// ensureBucketExists creates the bucket when it does not exist.
// HeadBucket is the idempotency check: a 200 response means the bucket
// already exists in this account and is accessible; no create is needed.
func ensureBucketExists(ctx context.Context, client S3API, name, region string) error {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: sdkaws.String(name),
	})
	if err == nil {
		// Bucket exists and is accessible — nothing to do.
		return nil
	}

	// SDK v2 returns *s3types.NotFound for HTTP 404 on HeadBucket.
	var notFound *s3types.NotFound
	if !errors.As(err, &notFound) {
		// Any other error (403 Forbidden, network failure, etc.) is terminal.
		return fmt.Errorf("checking bucket %s: %w", name, err)
	}

	// Bucket does not exist — create it.
	//
	// S3 quirk: us-east-1 rejects CreateBucketConfiguration entirely;
	// all other regions require a LocationConstraint.
	input := &s3.CreateBucketInput{
		Bucket: sdkaws.String(name),
	}
	if region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}

	_, createErr := client.CreateBucket(ctx, input)
	if createErr != nil {
		// BucketAlreadyOwnedByYou means a concurrent run beat us to it.
		// The bucket exists in this account — treat as success.
		var owned *s3types.BucketAlreadyOwnedByYou
		if !errors.As(createErr, &owned) {
			return fmt.Errorf("creating bucket %s: %w", name, createErr)
		}
	}

	return nil
}

// enableVersioning sets versioning to Enabled. This is a PUT operation and
// is safe to call even when versioning is already enabled.
func enableVersioning(ctx context.Context, client S3API, name string) error {
	_, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: sdkaws.String(name),
		VersioningConfiguration: &s3types.VersioningConfiguration{
			Status: s3types.BucketVersioningStatusEnabled,
		},
	})
	return err
}

// blockPublicAccess enables all four public-access block settings.
// This is a PUT operation and is safe to call on an already-blocked bucket.
func blockPublicAccess(ctx context.Context, client S3API, name string) error {
	_, err := client.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
		Bucket: sdkaws.String(name),
		PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
			BlockPublicAcls:       sdkaws.Bool(true),
			BlockPublicPolicy:     sdkaws.Bool(true),
			IgnorePublicAcls:      sdkaws.Bool(true),
			RestrictPublicBuckets: sdkaws.Bool(true),
		},
	})
	return err
}

// tagBucket applies the given tags to the bucket, overwriting the entire tag
// set. This is a PUT operation and is safe to call on an already-tagged bucket.
func tagBucket(ctx context.Context, client S3API, name string, tags map[string]string) error {
	s3Tags := make([]s3types.Tag, 0, len(tags))
	for k, v := range tags {
		s3Tags = append(s3Tags, s3types.Tag{
			Key:   sdkaws.String(k),
			Value: sdkaws.String(v),
		})
	}
	_, err := client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket:  sdkaws.String(name),
		Tagging: &s3types.Tagging{TagSet: s3Tags},
	})
	if err != nil {
		return fmt.Errorf("tagging bucket %s: %w", name, err)
	}
	return nil
}
