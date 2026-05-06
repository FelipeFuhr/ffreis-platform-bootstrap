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

// DeleteStateBucket empties the versioned S3 bucket (removing all object
// versions and delete markers) and then deletes the bucket itself.
//
// Idempotency: if the bucket does not exist, returns nil immediately.
// A versioned bucket cannot be deleted until it is empty; this function
// handles the emptying in batches of up to 1000 objects per API call.
func DeleteStateBucket(ctx context.Context, client S3API, name string) error {
	// Check whether the bucket exists first — skip if absent.
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: sdkaws.String(name)})
	if err != nil {
		var notFound *s3types.NotFound
		if errors.As(err, &notFound) {
			return nil // already gone
		}
		return fmt.Errorf("checking bucket %s before delete: %w", name, err)
	}

	// Empty all object versions and delete markers.
	if err := emptyBucket(ctx, client, name); err != nil {
		return fmt.Errorf("emptying bucket %s: %w", name, err)
	}

	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: sdkaws.String(name)})
	if err != nil {
		return fmt.Errorf("deleting bucket %s: %w", name, err)
	}
	return nil
}

// emptyBucket deletes all object versions and delete markers from a versioned
// bucket, paging through ListObjectVersions until IsTruncated is false.
func emptyBucket(ctx context.Context, client S3API, name string) error {
	var keyMarker, versionMarker *string

	for {
		out, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket:          sdkaws.String(name),
			KeyMarker:       keyMarker,
			VersionIdMarker: versionMarker,
		})
		if err != nil {
			return fmt.Errorf("listing versions: %w", err)
		}

		if err := deleteObjectPage(ctx, client, name, collectObjectIDs(out.Versions, out.DeleteMarkers)); err != nil {
			return err
		}

		if !sdkaws.ToBool(out.IsTruncated) {
			break
		}
		keyMarker = out.NextKeyMarker
		versionMarker = out.NextVersionIdMarker
	}

	return nil
}

func deleteObjectPage(ctx context.Context, client S3API, name string, objects []s3types.ObjectIdentifier) error {
	if len(objects) == 0 {
		return nil
	}

	delOut, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: sdkaws.String(name),
		Delete: &s3types.Delete{Objects: objects, Quiet: sdkaws.Bool(true)},
	})
	if err != nil {
		return fmt.Errorf("batch deleting objects: %w", err)
	}
	if len(delOut.Errors) == 0 {
		return nil
	}

	return fmt.Errorf("deleting objects: %s: %s",
		sdkaws.ToString(delOut.Errors[0].Key),
		sdkaws.ToString(delOut.Errors[0].Message))
}

// collectObjectIDs builds a flat list of ObjectIdentifiers from ListObjectVersions output.
func collectObjectIDs(versions []s3types.ObjectVersion, markers []s3types.DeleteMarkerEntry) []s3types.ObjectIdentifier {
	ids := make([]s3types.ObjectIdentifier, 0, len(versions)+len(markers))
	for _, v := range versions {
		ids = append(ids, s3types.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
	}
	for _, d := range markers {
		ids = append(ids, s3types.ObjectIdentifier{Key: d.Key, VersionId: d.VersionId})
	}
	return ids
}

// DeleteDynamoDBTable deletes the named DynamoDB table.
//
// Idempotency: if the table does not exist, returns nil immediately.
func DeleteDynamoDBTable(ctx context.Context, client DynamoDBAPI, name string) error {
	_, err := client.DeleteTable(ctx, &dynamodb.DeleteTableInput{
		TableName: sdkaws.String(name),
	})
	if err != nil {
		var notFound *dbtypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil // already gone
		}
		return fmt.Errorf("deleting DynamoDB table %s: %w", name, err)
	}
	return nil
}

// DeleteIAMRole removes all inline policies from the role and then deletes it.
//
// Idempotency: if the role does not exist, returns nil immediately.
// IAM requires all inline policies to be deleted before the role can be removed.
func DeleteIAMRole(ctx context.Context, client IAMAPI, roleName string) error {
	// Check existence first.
	_, err := client.GetRole(ctx, &iam.GetRoleInput{RoleName: sdkaws.String(roleName)})
	if err != nil {
		var notFound *iamtypes.NoSuchEntityException
		if errors.As(err, &notFound) {
			return nil // already gone
		}
		return fmt.Errorf("checking IAM role %s before delete: %w", roleName, err)
	}

	// List and delete all inline policies.
	out, err := client.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
		RoleName: sdkaws.String(roleName),
	})
	if err != nil {
		return fmt.Errorf("listing inline policies for role %s: %w", roleName, err)
	}
	for _, policyName := range out.PolicyNames {
		_, err := client.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
			RoleName:   sdkaws.String(roleName),
			PolicyName: sdkaws.String(policyName),
		})
		if err != nil {
			return fmt.Errorf("deleting inline policy %s from role %s: %w", policyName, roleName, err)
		}
	}

	_, err = client.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: sdkaws.String(roleName)})
	if err != nil {
		return fmt.Errorf("deleting IAM role %s: %w", roleName, err)
	}
	return nil
}

// DeleteSNSTopic deletes the SNS topic by ARN.
//
// The ARN is constructed from the account ID, region, and topic name.
// Idempotency: if the topic does not exist, returns nil immediately.
func DeleteSNSTopic(ctx context.Context, client SNSAPI, region, accountID, topicName string) error {
	topicARN := fmt.Sprintf(SNSTopicARNFormat, region, accountID, topicName)

	_, err := client.DeleteTopic(ctx, &sns.DeleteTopicInput{
		TopicArn: sdkaws.String(topicARN),
	})
	if err != nil {
		var notFound *snstypes.NotFoundException
		if errors.As(err, &notFound) {
			return nil // already gone
		}
		return fmt.Errorf("deleting SNS topic %s: %w", topicARN, err)
	}
	return nil
}

// DeleteAdminUser removes all access keys, the inline policy, and the
// persistent IAM admin user created by CreateAdminUser.
//
// Idempotency: if the user (or any component) does not exist, returns nil.
// IAM requires all access keys and policies to be removed before the user
// can be deleted; this function handles that ordering.
func DeleteAdminUser(ctx context.Context, client IAMAPI, userName string) error {
	// List and delete ALL access keys — DeleteUser fails if any key remains.
	keys, err := client.ListAccessKeys(ctx, &iam.ListAccessKeysInput{
		UserName: sdkaws.String(userName),
	})
	if err != nil {
		if isNoSuchEntity(err) {
			return nil // user does not exist; nothing to do
		}
		return fmt.Errorf("listing admin user access keys: %w", err)
	}
	if keys != nil {
		for _, k := range keys.AccessKeyMetadata {
			if _, delErr := client.DeleteAccessKey(ctx, &iam.DeleteAccessKeyInput{
				UserName:    sdkaws.String(userName),
				AccessKeyId: k.AccessKeyId,
			}); delErr != nil && !isNoSuchEntity(delErr) {
				return fmt.Errorf("deleting admin user access key: %w", delErr)
			}
		}
	}

	if _, err := client.DeleteUserPolicy(ctx, &iam.DeleteUserPolicyInput{
		UserName:   sdkaws.String(userName),
		PolicyName: sdkaws.String(adminUserPolicyName),
	}); err != nil && !isNoSuchEntity(err) {
		return fmt.Errorf("deleting admin user policy: %w", err)
	}

	if _, err := client.DeleteUser(ctx, &iam.DeleteUserInput{
		UserName: sdkaws.String(userName),
	}); err != nil && !isNoSuchEntity(err) {
		return fmt.Errorf("deleting admin user: %w", err)
	}

	return nil
}

// DeleteBudget deletes the named AWS Budget for the account.
//
// Idempotency: if the budget does not exist, returns nil immediately.
func DeleteBudget(ctx context.Context, client BudgetsAPI, accountID, budgetName string) error {
	_, err := client.DeleteBudget(ctx, &budgets.DeleteBudgetInput{
		AccountId:  sdkaws.String(accountID),
		BudgetName: sdkaws.String(budgetName),
	})
	if err != nil {
		var notFound *budgetstypes.NotFoundException
		if errors.As(err, &notFound) {
			return nil // already gone
		}
		return fmt.Errorf("deleting budget %s: %w", budgetName, err)
	}
	return nil
}
