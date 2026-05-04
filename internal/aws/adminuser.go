package aws

import (
	"context"
	"errors"
	"fmt"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

const (
	adminUserPolicyName = "assume-platform-admin"
)

// AdminUser holds the identity and credentials for the persistent IAM admin user
// created by bootstrap. This user can assume the platform-admin role for all
// subsequent platform operations.
type AdminUser struct {
	UserName        string
	AccessKeyID     string
	SecretAccessKey string
}

// CreateAdminUser creates a persistent IAM admin user with an inline policy
// that allows sts:AssumeRole on the platform-admin role ARN.
//
// If the user already exists (e.g. from a previous bootstrap run), the existing
// user is reused: any existing access keys are deleted first (IAM users are
// limited to 2 keys), the policy is overwritten, and a fresh access key is created.
// This makes the function idempotent and safe to re-run.
func CreateAdminUser(ctx context.Context, client IAMAPI, userName, roleARN string, tags map[string]string) (*AdminUser, error) {
	if err := ensureAdminUserExists(ctx, client, userName, tags); err != nil {
		return nil, err
	}

	// Delete any existing access keys before creating a new one.
	// IAM users are limited to 2 access keys; orphaned keys from a previous
	// run would cause CreateAccessKey to fail.
	existingKeys, err := client.ListAccessKeys(ctx, &iam.ListAccessKeysInput{
		UserName: sdkaws.String(userName),
	})
	if err != nil {
		return nil, fmt.Errorf("listing admin user access keys: %w", err)
	}
	if existingKeys != nil {
		for _, k := range existingKeys.AccessKeyMetadata {
			if _, delErr := client.DeleteAccessKey(ctx, &iam.DeleteAccessKeyInput{
				UserName:    sdkaws.String(userName),
				AccessKeyId: k.AccessKeyId,
			}); delErr != nil && !isNoSuchEntity(delErr) {
				return nil, fmt.Errorf("deleting orphaned admin user access key: %w", delErr)
			}
		}
	}

	if err := putAdminUserPolicy(ctx, client, userName, roleARN); err != nil {
		return nil, err
	}

	key, err := client.CreateAccessKey(ctx, &iam.CreateAccessKeyInput{
		UserName: sdkaws.String(userName),
	})
	if err != nil {
		return nil, fmt.Errorf("creating access key for admin user: %w", err)
	}

	return &AdminUser{
		UserName:        userName,
		AccessKeyID:     sdkaws.ToString(key.AccessKey.AccessKeyId),
		SecretAccessKey: sdkaws.ToString(key.AccessKey.SecretAccessKey),
	}, nil
}

// ensureAdminUserExists creates the admin user if it does not already exist.
func ensureAdminUserExists(ctx context.Context, client IAMAPI, userName string, tags map[string]string) error {
	_, err := client.GetUser(ctx, &iam.GetUserInput{
		UserName: sdkaws.String(userName),
	})
	if err == nil {
		return nil // already exists
	}
	if !isNoSuchEntity(err) {
		return fmt.Errorf("checking admin user: %w", err)
	}

	iamTags := make([]iamtypes.Tag, 0, len(tags))
	for k, v := range tags {
		iamTags = append(iamTags, iamtypes.Tag{
			Key:   sdkaws.String(k),
			Value: sdkaws.String(v),
		})
	}

	_, createErr := client.CreateUser(ctx, &iam.CreateUserInput{
		UserName: sdkaws.String(userName),
		Tags:     iamTags,
	})
	if createErr != nil {
		var exists *iamtypes.EntityAlreadyExistsException
		if errors.As(createErr, &exists) {
			return nil // concurrent run created it
		}
		return fmt.Errorf("creating admin user: %w", createErr)
	}

	return nil
}

// putAdminUserPolicy attaches (or replaces) an inline policy that allows only
// sts:AssumeRole on the platform-admin role ARN.
func putAdminUserPolicy(ctx context.Context, client IAMAPI, userName, roleARN string) error {
	doc, err := marshalPolicy(policyDocument{
		Version: "2012-10-17",
		Statement: []policyStatement{
			{
				Effect:   "Allow",
				Action:   "sts:AssumeRole",
				Resource: roleARN,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("marshalling admin user policy: %w", err)
	}

	_, err = client.PutUserPolicy(ctx, &iam.PutUserPolicyInput{
		UserName:       sdkaws.String(userName),
		PolicyName:     sdkaws.String(adminUserPolicyName),
		PolicyDocument: sdkaws.String(doc),
	})
	if err != nil {
		return fmt.Errorf("putting admin user policy: %w", err)
	}
	return nil
}
