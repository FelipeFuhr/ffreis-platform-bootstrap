package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// EnsurePlatformAdminRole creates the platform-admin IAM role if it does not
// exist and always applies the inline permissions policy.
//
// Calling this function multiple times is safe:
//   - Role already exists → CreateRole is skipped, PutRolePolicy still runs.
//   - Trust policy is always re-applied, so drifted trust is healed in place.
//   - PutRolePolicy is an idempotent PUT; re-applying the same policy is a no-op.
//   - TagRole is idempotent — re-applying the same tags is safe.
//
// Trust:       arn:aws:iam::{accountID}:root  (full account principal)
// Permissions: allow * on * except a deny list of root-account-level actions.
// tags is applied when non-empty; pass nil to skip tagging.
func EnsurePlatformAdminRole(ctx context.Context, client IAMAPI, roleName, accountID string, tags map[string]string) error {
	if err := ensureRoleExists(ctx, client, roleName, accountID); err != nil {
		return err
	}
	if err := updateTrustPolicy(ctx, client, roleName, accountID); err != nil {
		return fmt.Errorf("updating trust policy on role %s: %w", roleName, err)
	}
	if err := putAdminPolicy(ctx, client, roleName); err != nil {
		return fmt.Errorf("putting inline policy on role %s: %w", roleName, err)
	}
	if len(tags) > 0 {
		if err := tagIAMRole(ctx, client, roleName, tags); err != nil {
			return err
		}
	}
	return nil
}

// updateTrustPolicy always reapplies the assume-role trust policy so a role
// left behind in a drifted state can be repaired by a normal bootstrap run.
func updateTrustPolicy(ctx context.Context, client IAMAPI, roleName, accountID string) error {
	trustDoc, err := marshalPolicy(buildTrustPolicy(accountID))
	if err != nil {
		return fmt.Errorf("marshalling trust policy: %w", err)
	}

	_, err = client.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
		RoleName:       sdkaws.String(roleName),
		PolicyDocument: sdkaws.String(trustDoc),
	})
	return err
}

// ensureRoleExists creates the role when it does not exist.
// An EntityAlreadyExists response from CreateRole is treated as success so
// that concurrent or repeated runs do not fail.
func ensureRoleExists(ctx context.Context, client IAMAPI, roleName, accountID string) error {
	_, err := client.GetRole(ctx, &iam.GetRoleInput{
		RoleName: sdkaws.String(roleName),
	})
	if err == nil {
		// Role exists — nothing to create.
		return nil
	}

	var notFound *iamtypes.NoSuchEntityException
	if !errors.As(err, &notFound) {
		return fmt.Errorf("checking role %s: %w", roleName, err)
	}

	trustDoc, err := marshalPolicy(buildTrustPolicy(accountID))
	if err != nil {
		return fmt.Errorf("marshalling trust policy: %w", err)
	}

	_, createErr := client.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 sdkaws.String(roleName),
		AssumeRolePolicyDocument: sdkaws.String(trustDoc),
		Description:              sdkaws.String("Platform administration role. Trusted by the account root principal."),
	})
	if createErr != nil {
		// A concurrent run may have created the role between our GetRole and
		// CreateRole calls. Treat EntityAlreadyExists as success.
		var exists *iamtypes.EntityAlreadyExistsException
		if !errors.As(createErr, &exists) {
			return fmt.Errorf("creating role %s: %w", roleName, createErr)
		}
	}

	return nil
}

// putAdminPolicy attaches the inline permissions policy to the role.
// PutRolePolicy is a PUT operation: it creates the policy if absent and
// overwrites it if present, making it safe to call on every run.
func putAdminPolicy(ctx context.Context, client IAMAPI, roleName string) error {
	permDoc, err := marshalPolicy(buildPermissionsPolicy())
	if err != nil {
		return fmt.Errorf("marshalling permissions policy: %w", err)
	}

	_, err = client.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       sdkaws.String(roleName),
		PolicyName:     sdkaws.String(roleName + "-policy"),
		PolicyDocument: sdkaws.String(permDoc),
	})
	return err
}

// buildTrustPolicy returns a trust-policy document that allows the account
// root principal to assume the role. Any principal in the account can then
// be granted sts:AssumeRole by their own IAM policy.
func buildTrustPolicy(accountID string) policyDocument {
	return policyDocument{
		Version: "2012-10-17",
		Statement: []policyStatement{
			{
				Effect:    "Allow",
				Principal: map[string]string{"AWS": fmt.Sprintf(IAMRootPrincipalARNFormat, accountID)},
				Action:    "sts:AssumeRole",
			},
		},
	}
}

// buildPermissionsPolicy returns a policy that allows all actions and then
// explicitly denies the subset of actions that modify root-account-level
// configuration. This keeps the role powerful without giving it the ability
// to lock out or destroy the account itself.
//
// Deny list rationale:
//   - iam:*VirtualMFADevice        prevent removing/adding root MFA
//   - iam:*AccountPasswordPolicy   account-wide password policy is root scope
//   - account:CloseAccount         irreversible — root only
//   - account:Put/DeleteAlternate* billing/ops contacts are root scope
//   - account:Put/DeleteContact*   primary contact is root scope
//   - account:Enable/DisableRegion opt-in regions should be a deliberate act
func buildPermissionsPolicy() policyDocument {
	return policyDocument{
		Version: "2012-10-17",
		Statement: []policyStatement{
			{
				Sid:      "AllowAll",
				Effect:   "Allow",
				Action:   "*",
				Resource: "*",
			},
			{
				Sid:    "DenyRootAccountChanges",
				Effect: "Deny",
				Action: []string{
					"iam:CreateVirtualMFADevice",
					"iam:DeleteVirtualMFADevice",
					"iam:UpdateAccountPasswordPolicy",
					"iam:DeleteAccountPasswordPolicy",
					"account:CloseAccount",
					"account:PutAlternateContact",
					"account:DeleteAlternateContact",
					"account:PutContactInformation",
					"account:DeleteContactInformation",
					"account:EnableRegion",
					"account:DisableRegion",
				},
				Resource: "*",
			},
		},
	}
}

// ---- policy document types ----

type policyDocument struct {
	Version   string            `json:"Version"`
	Statement []policyStatement `json:"Statement"`
}

type policyStatement struct {
	Sid       string                       `json:"Sid,omitempty"`
	Effect    string                       `json:"Effect"`
	Principal interface{}                  `json:"Principal,omitempty"`
	Action    interface{}                  `json:"Action"` // string or []string
	Resource  interface{}                  `json:"Resource,omitempty"`
	Condition map[string]map[string]string `json:"Condition,omitempty"`
}

// tagIAMRole applies tags to the IAM role. TagRole is idempotent — re-applying
// the same tags overwrites the values with identical ones, which is safe.
func tagIAMRole(ctx context.Context, client IAMAPI, roleName string, tags map[string]string) error {
	iamTags := make([]iamtypes.Tag, 0, len(tags))
	for k, v := range tags {
		iamTags = append(iamTags, iamtypes.Tag{
			Key:   sdkaws.String(k),
			Value: sdkaws.String(v),
		})
	}
	_, err := client.TagRole(ctx, &iam.TagRoleInput{
		RoleName: sdkaws.String(roleName),
		Tags:     iamTags,
	})
	if err != nil {
		return fmt.Errorf("tagging IAM role %s: %w", roleName, err)
	}
	return nil
}

func marshalPolicy(doc policyDocument) (string, error) {
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
