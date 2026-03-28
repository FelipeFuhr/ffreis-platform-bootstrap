package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/budgets"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// CallerIdentityGetter is the subset of STS operations used to verify caller identity.
// *sts.Client satisfies this interface.
type CallerIdentityGetter interface {
	GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// S3API is the subset of S3 operations used by EnsureStateBucket, DeleteStateBucket, and doctor.
// *s3.Client satisfies this interface; use it in tests with a mock.
type S3API interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	ListBuckets(ctx context.Context, params *s3.ListBucketsInput, optFns ...func(*s3.Options)) (*s3.ListBucketsOutput, error)
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	PutBucketVersioning(ctx context.Context, params *s3.PutBucketVersioningInput, optFns ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error)
	PutPublicAccessBlock(ctx context.Context, params *s3.PutPublicAccessBlockInput, optFns ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error)
	PutBucketTagging(ctx context.Context, params *s3.PutBucketTaggingInput, optFns ...func(*s3.Options)) (*s3.PutBucketTaggingOutput, error)
	ListObjectVersions(ctx context.Context, params *s3.ListObjectVersionsInput, optFns ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	DeleteBucket(ctx context.Context, params *s3.DeleteBucketInput, optFns ...func(*s3.Options)) (*s3.DeleteBucketOutput, error)
}

// DynamoDBAPI is the subset of DynamoDB operations used by EnsureLockTable,
// EnsureRegistryTable, RegisterResource, DeleteDynamoDBTable, and doctor.
// *dynamodb.Client satisfies this interface.
type DynamoDBAPI interface {
	DescribeTable(ctx context.Context, params *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	ListTables(ctx context.Context, params *dynamodb.ListTablesInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error)
	CreateTable(ctx context.Context, params *dynamodb.CreateTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error)
	TagResource(ctx context.Context, params *dynamodb.TagResourceInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	DeleteTable(ctx context.Context, params *dynamodb.DeleteTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error)
}

// IAMAPI is the subset of IAM operations used by EnsurePlatformAdminRole, DeleteIAMRole, and doctor.
// *iam.Client satisfies this interface.
type IAMAPI interface {
	GetRole(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	GetAccountSummary(ctx context.Context, params *iam.GetAccountSummaryInput, optFns ...func(*iam.Options)) (*iam.GetAccountSummaryOutput, error)
	CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	PutRolePolicy(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error)
	TagRole(ctx context.Context, params *iam.TagRoleInput, optFns ...func(*iam.Options)) (*iam.TagRoleOutput, error)
	ListRolePolicies(ctx context.Context, params *iam.ListRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error)
	DeleteRolePolicy(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error)
	DeleteRole(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error)
}

// SNSAPI is the subset of SNS operations used by EnsureEventsTopic, PublishEvent, DeleteSNSTopic, and doctor.
// *sns.Client satisfies this interface.
type SNSAPI interface {
	CreateTopic(ctx context.Context, params *sns.CreateTopicInput, optFns ...func(*sns.Options)) (*sns.CreateTopicOutput, error)
	ListTopics(ctx context.Context, params *sns.ListTopicsInput, optFns ...func(*sns.Options)) (*sns.ListTopicsOutput, error)
	Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
	TagResource(ctx context.Context, params *sns.TagResourceInput, optFns ...func(*sns.Options)) (*sns.TagResourceOutput, error)
	GetTopicAttributes(ctx context.Context, params *sns.GetTopicAttributesInput, optFns ...func(*sns.Options)) (*sns.GetTopicAttributesOutput, error)
	SetTopicAttributes(ctx context.Context, params *sns.SetTopicAttributesInput, optFns ...func(*sns.Options)) (*sns.SetTopicAttributesOutput, error)
	DeleteTopic(ctx context.Context, params *sns.DeleteTopicInput, optFns ...func(*sns.Options)) (*sns.DeleteTopicOutput, error)
}

// BudgetsAPI is the subset of AWS Budgets operations used by EnsureBudget, DeleteBudget, and doctor.
// *budgets.Client satisfies this interface.
type BudgetsAPI interface {
	CreateBudget(ctx context.Context, params *budgets.CreateBudgetInput, optFns ...func(*budgets.Options)) (*budgets.CreateBudgetOutput, error)
	DescribeBudget(ctx context.Context, params *budgets.DescribeBudgetInput, optFns ...func(*budgets.Options)) (*budgets.DescribeBudgetOutput, error)
	DescribeBudgets(ctx context.Context, params *budgets.DescribeBudgetsInput, optFns ...func(*budgets.Options)) (*budgets.DescribeBudgetsOutput, error)
	DeleteBudget(ctx context.Context, params *budgets.DeleteBudgetInput, optFns ...func(*budgets.Options)) (*budgets.DeleteBudgetOutput, error)
}
