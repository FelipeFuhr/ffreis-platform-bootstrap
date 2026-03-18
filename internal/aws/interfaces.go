package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/budgets"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

// S3API is the subset of S3 operations used by EnsureStateBucket.
// *s3.Client satisfies this interface; use it in tests with a mock.
type S3API interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	PutBucketVersioning(ctx context.Context, params *s3.PutBucketVersioningInput, optFns ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error)
	PutPublicAccessBlock(ctx context.Context, params *s3.PutPublicAccessBlockInput, optFns ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error)
	PutBucketTagging(ctx context.Context, params *s3.PutBucketTaggingInput, optFns ...func(*s3.Options)) (*s3.PutBucketTaggingOutput, error)
}

// DynamoDBAPI is the subset of DynamoDB operations used by EnsureLockTable,
// EnsureRegistryTable, and RegisterResource.
// *dynamodb.Client satisfies this interface.
type DynamoDBAPI interface {
	DescribeTable(ctx context.Context, params *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	CreateTable(ctx context.Context, params *dynamodb.CreateTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error)
	TagResource(ctx context.Context, params *dynamodb.TagResourceInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
}

// IAMAPI is the subset of IAM operations used by EnsurePlatformAdminRole.
// *iam.Client satisfies this interface.
type IAMAPI interface {
	GetRole(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	PutRolePolicy(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error)
	TagRole(ctx context.Context, params *iam.TagRoleInput, optFns ...func(*iam.Options)) (*iam.TagRoleOutput, error)
}

// SNSAPI is the subset of SNS operations used by EnsureEventsTopic and PublishEvent.
// *sns.Client satisfies this interface.
type SNSAPI interface {
	CreateTopic(ctx context.Context, params *sns.CreateTopicInput, optFns ...func(*sns.Options)) (*sns.CreateTopicOutput, error)
	Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
	TagResource(ctx context.Context, params *sns.TagResourceInput, optFns ...func(*sns.Options)) (*sns.TagResourceOutput, error)
	GetTopicAttributes(ctx context.Context, params *sns.GetTopicAttributesInput, optFns ...func(*sns.Options)) (*sns.GetTopicAttributesOutput, error)
	SetTopicAttributes(ctx context.Context, params *sns.SetTopicAttributesInput, optFns ...func(*sns.Options)) (*sns.SetTopicAttributesOutput, error)
}

// BudgetsAPI is the subset of AWS Budgets operations used by EnsureBudget.
// *budgets.Client satisfies this interface.
type BudgetsAPI interface {
	CreateBudget(ctx context.Context, params *budgets.CreateBudgetInput, optFns ...func(*budgets.Options)) (*budgets.CreateBudgetOutput, error)
	DescribeBudget(ctx context.Context, params *budgets.DescribeBudgetInput, optFns ...func(*budgets.Options)) (*budgets.DescribeBudgetOutput, error)
}
