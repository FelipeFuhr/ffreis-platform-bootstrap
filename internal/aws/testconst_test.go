package aws

import testconstants "github.com/ffreis/platform-bootstrap/internal/test"

// Shared test constants for all internal/aws package tests.
const (
	// Error message templates.
	errUnexpectedFmt  = "unexpected error: %v"
	errExpectedGotNil = "expected error, got nil"
	errWantTrue       = "want true, got false"
	errWantFalse      = "want false, got true"

	// AWS region.
	testRegion = testconstants.RegionUSEast1

	// IAM.
	testRoleName = testconstants.RoleNamePlatformAdmin

	// DynamoDB.
	testLockTable = "test-table"
	testDynamoARN = "arn:aws:dynamodb:us-east-1:123:table/test-table"

	// S3.
	testS3Bucket = "test-bucket"

	// exists_test helpers.
	testExistsBucket    = "my-bucket"
	testExistsTable     = "my-table"
	testExistsTopicName = "platform-events"

	// SNS.
	testEventsTopicName = "ffreis-platform-events"
	testRootARN         = "arn:aws:iam::123:root"

	// Registry.
	testRegistryTable = "test-registry"
	testStateBucket   = "ffreis-tf-state-root"
	testCallerRoleARN = "arn:aws:iam::123:role/caller"
	testDevEmail      = "dev@example.com"

	// Session.
	testBootstrapARN = "arn:aws:iam::123456789012:user/bootstrap"
)
