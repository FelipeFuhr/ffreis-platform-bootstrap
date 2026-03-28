package aws

import (
	"context"
	"errors"
	"fmt"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	budgetstypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// BucketExists reports whether the S3 bucket is already present and
// accessible. It is used to distinguish "created" from "already existed"
// for event publishing and returns false on any error.
func (c *Clients) BucketExists(ctx context.Context, name string) bool {
	ok, err := c.BucketExistsChecked(ctx, name)
	return err == nil && ok
}

// BucketExistsChecked reports whether the S3 bucket exists.
// It returns (false, nil) when the bucket does not exist.
func (c *Clients) BucketExistsChecked(ctx context.Context, name string) (bool, error) {
	_, err := c.S3.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: sdkaws.String(name),
	})
	if err == nil {
		return true, nil
	}
	var notFound *s3types.NotFound
	if errors.As(err, &notFound) {
		return false, nil
	}
	return false, err
}

// TableExists reports whether the DynamoDB table exists and is ACTIVE.
// Returns false on any error or if the table is in a non-ACTIVE state.
func (c *Clients) TableExists(ctx context.Context, name string) bool {
	out, err := c.DynamoDB.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: sdkaws.String(name),
	})
	return err == nil && out.Table != nil && out.Table.TableStatus == "ACTIVE"
}

// TableExistsChecked reports whether the DynamoDB table exists.
// It returns (false, nil) when the table does not exist.
func (c *Clients) TableExistsChecked(ctx context.Context, name string) (bool, error) {
	out, err := c.DynamoDB.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: sdkaws.String(name),
	})
	if err == nil {
		return out.Table != nil, nil
	}
	var notFound *dbtypes.ResourceNotFoundException
	if errors.As(err, &notFound) {
		return false, nil
	}
	return false, err
}

// RoleExists reports whether the IAM role is already present.
// Returns false on any error.
func (c *Clients) RoleExists(ctx context.Context, name string) bool {
	ok, err := c.RoleExistsChecked(ctx, name)
	return err == nil && ok
}

// RoleExistsChecked reports whether the IAM role exists.
// It returns (false, nil) when the role does not exist.
func (c *Clients) RoleExistsChecked(ctx context.Context, name string) (bool, error) {
	_, err := c.IAM.GetRole(ctx, &iam.GetRoleInput{
		RoleName: sdkaws.String(name),
	})
	if err == nil {
		return true, nil
	}
	var noSuch *iamtypes.NoSuchEntityException
	if errors.As(err, &noSuch) {
		return false, nil
	}
	return false, err
}

// TopicExists reports whether the SNS topic with the given name exists in
// the account. The ARN is constructed from the account ID, region, and name.
// Returns false on any error.
func (c *Clients) TopicExists(ctx context.Context, name string) bool {
	ok, err := c.TopicExistsChecked(ctx, name)
	return err == nil && ok
}

// TopicExistsChecked reports whether the SNS topic with the given name exists.
// It returns (false, nil) when the topic does not exist.
func (c *Clients) TopicExistsChecked(ctx context.Context, name string) (bool, error) {
	topicARN := fmt.Sprintf(SNSTopicARNFormat, c.Region, c.AccountID, name)
	_, err := c.SNS.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{
		TopicArn: sdkaws.String(topicARN),
	})
	if err == nil {
		return true, nil
	}
	var notFound *snstypes.NotFoundException
	if errors.As(err, &notFound) {
		return false, nil
	}
	return false, err
}

// BudgetExists reports whether the named AWS Budget exists for the account.
// Returns false on any error.
func (c *Clients) BudgetExists(ctx context.Context, name string) bool {
	ok, err := c.BudgetExistsChecked(ctx, name)
	return err == nil && ok
}

// BudgetExistsChecked reports whether the named AWS Budget exists.
// It returns (false, nil) when the budget does not exist.
func (c *Clients) BudgetExistsChecked(ctx context.Context, name string) (bool, error) {
	_, err := c.Budgets.DescribeBudget(ctx, &budgets.DescribeBudgetInput{
		AccountId:  sdkaws.String(c.AccountID),
		BudgetName: sdkaws.String(name),
	})
	if err == nil {
		return true, nil
	}
	var notFound *budgetstypes.NotFoundException
	if errors.As(err, &notFound) {
		return false, nil
	}
	return false, err
}

// ResourceExists reports whether the named resource of the given type exists
// in AWS. It dispatches to the appropriate type-specific existence check.
// Returns false for unknown resource types.
func (c *Clients) ResourceExists(ctx context.Context, resourceType, resourceName string) bool {
	switch resourceType {
	case "S3Bucket":
		return c.BucketExists(ctx, resourceName)
	case "DynamoDBTable":
		return c.TableExists(ctx, resourceName)
	case "IAMRole":
		return c.RoleExists(ctx, resourceName)
	case "SNSTopic":
		return c.TopicExists(ctx, resourceName)
	case "AWSBudget":
		return c.BudgetExists(ctx, resourceName)
	}
	return false
}
