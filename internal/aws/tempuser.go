package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	sdkcfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	TempBootstrapUserName   = "platform-bootstrap-temp"
	tempBootstrapPolicyName = "platform-bootstrap-temp-policy"
)

// TempUser holds the identity and credentials for the short-lived IAM user
// created to bridge the root→platform-admin assumption gap.
// Root accounts cannot call sts:AssumeRole, so bootstrap creates this user,
// uses its credentials to assume platform-admin, then deletes it.
type TempUser struct {
	UserName        string
	AccessKeyID     string
	SecretAccessKey string
}

// CreateTempBootstrapUser creates a short-lived IAM user named
// "platform-bootstrap-temp" with a single inline policy that allows
// sts:AssumeRole on the platform-admin role ARN.
//
// If the user already exists (e.g. left over from a previous partial run),
// the existing user is reused: the policy is overwritten and a new access
// key is created. Callers should always call DeleteTempBootstrapUser to
// remove the user once role assumption succeeds.
func CreateTempBootstrapUser(ctx context.Context, client IAMAPI, roleARN string, tags map[string]string) (TempUser, error) {
	if err := ensureTempUserExists(ctx, client, tags); err != nil {
		return TempUser{}, err
	}

	if err := putTempUserPolicy(ctx, client, roleARN); err != nil {
		return TempUser{}, err
	}

	key, err := client.CreateAccessKey(ctx, &iam.CreateAccessKeyInput{
		UserName: sdkaws.String(TempBootstrapUserName),
	})
	if err != nil {
		return TempUser{}, fmt.Errorf("creating access key for temp user: %w", err)
	}

	return TempUser{
		UserName:        TempBootstrapUserName,
		AccessKeyID:     sdkaws.ToString(key.AccessKey.AccessKeyId),
		SecretAccessKey: sdkaws.ToString(key.AccessKey.SecretAccessKey),
	}, nil
}

// DeleteTempBootstrapUser deletes all access keys, the inline policy, and the
// IAM user created by CreateTempBootstrapUser.
//
// All access keys are listed and deleted — not just the one tracked in u —
// so that a re-run after a previous partial failure (which left an orphaned
// key) does not cause DeleteUser to fail with "must delete access keys first".
// It is safe to call when any component is already absent.
func DeleteTempBootstrapUser(ctx context.Context, client IAMAPI, u TempUser) error {
	// List and delete ALL access keys on the user. DeleteUser fails if any key
	// remains, and a partial previous run may have left an extra orphaned key.
	keys, err := client.ListAccessKeys(ctx, &iam.ListAccessKeysInput{
		UserName: sdkaws.String(u.UserName),
	})
	if err != nil && !isNoSuchEntity(err) {
		return fmt.Errorf("listing temp user access keys: %w", err)
	}
	if keys != nil {
		for _, k := range keys.AccessKeyMetadata {
			if _, delErr := client.DeleteAccessKey(ctx, &iam.DeleteAccessKeyInput{
				UserName:    sdkaws.String(u.UserName),
				AccessKeyId: k.AccessKeyId,
			}); delErr != nil && !isNoSuchEntity(delErr) {
				return fmt.Errorf("deleting temp user access key: %w", delErr)
			}
		}
	}

	if _, err := client.DeleteUserPolicy(ctx, &iam.DeleteUserPolicyInput{
		UserName:   sdkaws.String(u.UserName),
		PolicyName: sdkaws.String(tempBootstrapPolicyName),
	}); err != nil && !isNoSuchEntity(err) {
		return fmt.Errorf("deleting temp user policy: %w", err)
	}

	if _, err := client.DeleteUser(ctx, &iam.DeleteUserInput{
		UserName: sdkaws.String(u.UserName),
	}); err != nil && !isNoSuchEntity(err) {
		return fmt.Errorf("deleting temp user: %w", err)
	}

	return nil
}

// tempUserAssumeRetryDelays defines the wait intervals between AssumeRole
// attempts when using fresh temp-user credentials. AWS IAM credential and
// policy propagation typically takes 5–15 seconds; retrying with increasing
// backoff avoids a hard failure on the first attempt while bounding the
// total wait to roughly one minute.
var tempUserAssumeRetryDelays = []time.Duration{
	3 * time.Second,
	5 * time.Second,
	8 * time.Second,
	10 * time.Second,
	12 * time.Second,
	15 * time.Second,
}

// AssumeRoleWithTempUser builds a new Clients bundle by authenticating with
// the temp user's static credentials and then assuming the given roleARN.
// The returned Clients has CallerARN set to the assumed-role session ARN and
// AccountID preserved from the original Clients.
//
// AWS IAM credential and policy propagation is eventually consistent; fresh
// access keys often return InvalidClientTokenId or AccessDenied for the first
// few seconds. AssumeRoleWithTempUser retries with backoff until propagation
// completes or the retry budget is exhausted.
func AssumeRoleWithTempUser(ctx context.Context, orig *Clients, u TempUser, roleARN string) (*Clients, error) {
	awsCfg, err := sdkcfg.LoadDefaultConfig(ctx,
		sdkcfg.WithRegion(orig.Region),
		sdkcfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(u.AccessKeyID, u.SecretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("building config for temp user: %w", err)
	}

	tempClients := &Clients{
		STSRoleAssumer: sts.NewFromConfig(awsCfg),
		Region:         orig.Region,
		AccountID:      orig.AccountID,
		Profile:        orig.Profile,
	}

	var lastErr error
	for attempt := 0; ; attempt++ {
		result, assumeErr := AssumeAdminRole(ctx, tempClients, roleARN)
		if assumeErr == nil {
			return result, nil
		}
		if !isTempUserPropagationError(assumeErr) || attempt >= len(tempUserAssumeRetryDelays) {
			return nil, assumeErr
		}
		lastErr = assumeErr
		delay := tempUserAssumeRetryDelays[attempt]
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled while waiting for IAM propagation: %w (last error: %v)", ctx.Err(), lastErr)
		case <-time.After(delay):
		}
	}
}

// isTempUserPropagationError reports whether err is a transient IAM/STS
// error caused by credential or policy propagation delay. Both
// InvalidClientTokenId (credentials not yet valid) and AccessDenied (policy
// not yet visible) can appear in the seconds after a new access key or inline
// policy is created.
func isTempUserPropagationError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "InvalidClientTokenId") ||
		strings.Contains(msg, "AccessDenied")
}

// ensureTempUserExists creates the temp user if it does not already exist.
func ensureTempUserExists(ctx context.Context, client IAMAPI, tags map[string]string) error {
	_, err := client.GetUser(ctx, &iam.GetUserInput{
		UserName: sdkaws.String(TempBootstrapUserName),
	})
	if err == nil {
		return nil // already exists
	}
	if !isNoSuchEntity(err) {
		return fmt.Errorf("checking temp user: %w", err)
	}

	iamTags := make([]iamtypes.Tag, 0, len(tags))
	for k, v := range tags {
		iamTags = append(iamTags, iamtypes.Tag{
			Key:   sdkaws.String(k),
			Value: sdkaws.String(v),
		})
	}

	_, createErr := client.CreateUser(ctx, &iam.CreateUserInput{
		UserName: sdkaws.String(TempBootstrapUserName),
		Tags:     iamTags,
	})
	if createErr != nil {
		var exists *iamtypes.EntityAlreadyExistsException
		if errors.As(createErr, &exists) {
			return nil // concurrent run created it
		}
		return fmt.Errorf("creating temp user: %w", createErr)
	}

	return nil
}

// putTempUserPolicy attaches (or replaces) an inline policy that allows only
// sts:AssumeRole on the platform-admin role ARN.
func putTempUserPolicy(ctx context.Context, client IAMAPI, roleARN string) error {
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
		return fmt.Errorf("marshalling temp user policy: %w", err)
	}

	_, err = client.PutUserPolicy(ctx, &iam.PutUserPolicyInput{
		UserName:       sdkaws.String(TempBootstrapUserName),
		PolicyName:     sdkaws.String(tempBootstrapPolicyName),
		PolicyDocument: sdkaws.String(doc),
	})
	if err != nil {
		return fmt.Errorf("putting temp user policy: %w", err)
	}
	return nil
}

// isNoSuchEntity reports whether err is an IAM NoSuchEntityException.
func isNoSuchEntity(err error) bool {
	var nse *iamtypes.NoSuchEntityException
	return errors.As(err, &nse)
}
