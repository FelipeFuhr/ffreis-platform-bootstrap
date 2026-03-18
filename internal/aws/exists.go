package aws

import (
	"context"
	"fmt"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

// BucketExists reports whether the S3 bucket is already present and
// accessible. It is used to distinguish "created" from "already existed"
// for event publishing and returns false on any error.
func (c *Clients) BucketExists(ctx context.Context, name string) bool {
	_, err := c.S3.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: sdkaws.String(name),
	})
	return err == nil
}

// TableExists reports whether the DynamoDB table exists and is ACTIVE.
// Returns false on any error or if the table is in a non-ACTIVE state.
func (c *Clients) TableExists(ctx context.Context, name string) bool {
	out, err := c.DynamoDB.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: sdkaws.String(name),
	})
	return err == nil && out.Table.TableStatus == "ACTIVE"
}

// RoleExists reports whether the IAM role is already present.
// Returns false on any error.
func (c *Clients) RoleExists(ctx context.Context, name string) bool {
	_, err := c.IAM.GetRole(ctx, &iam.GetRoleInput{
		RoleName: sdkaws.String(name),
	})
	return err == nil
}

// TopicExists reports whether the SNS topic with the given name exists in
// the account. The ARN is constructed from the account ID, region, and name.
// Returns false on any error.
func (c *Clients) TopicExists(ctx context.Context, name string) bool {
	topicARN := fmt.Sprintf("arn:aws:sns:%s:%s:%s", c.Region, c.AccountID, name)
	_, err := c.SNS.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{
		TopicArn: sdkaws.String(topicARN),
	})
	return err == nil
}

// BudgetExists reports whether the named AWS Budget exists for the account.
// Returns false on any error.
func (c *Clients) BudgetExists(ctx context.Context, name string) bool {
	_, err := c.Budgets.DescribeBudget(ctx, &budgets.DescribeBudgetInput{
		AccountId:  sdkaws.String(c.AccountID),
		BudgetName: sdkaws.String(name),
	})
	return err == nil
}
