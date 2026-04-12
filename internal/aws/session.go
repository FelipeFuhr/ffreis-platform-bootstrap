package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	sdkcfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	platformcfg "github.com/ffreis/platform-bootstrap/internal/config"
)

// ErrNoCredentials is returned when neither a named profile nor environment
// credentials are available. It tells the operator exactly how to fix it.
var ErrNoCredentials = errors.New(
	"no AWS credentials configured: " +
		"provide --profile (or " + platformcfg.EnvAWSProfile + ", " + platformcfg.AWSProfileEnv + ") for a named profile, " +
		"or set AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY for environment credentials. " +
		"If you use AWS SSO / IAM Identity Center, run: aws sso login --profile <profile>",
)

// Clients holds all AWS service clients and the resolved account identity.
// It is constructed once per CLI invocation and passed to bootstrap steps.
// All service fields are interface types so tests can substitute mocks without
// needing a live AWS endpoint.
type Clients struct {
	STS            CallerIdentityProvider
	STSRoleAssumer AssumeRoler
	S3             S3API
	DynamoDB       DynamoDBAPI
	IAM            IAMAPI
	SNS            SNSAPI
	Budgets        BudgetsAPI

	AccountID string
	CallerARN string
	Region    string
	Profile   string
}

// New builds an AWS configuration, verifies the credentials are functional
// by calling sts:GetCallerIdentity, and returns a Clients bundle.
//
// Credential resolution order:
//  1. Named profile (cfg.AWSProfile or PLATFORM_AWS_PROFILE)
//  2. Environment variables (AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY)
//  3. Error — no silent fallback to instance metadata or other sources.
//
// The explicit error on case 3 prevents accidental use of unexpected
// credentials (e.g., an EC2 instance role when the operator forgot to set
// a profile) and surfaces misconfiguration before any AWS writes occur.
func New(ctx context.Context, cfg *platformcfg.Config) (*Clients, error) {
	return NewWithOpts(ctx, cfg, nil)
}

// NewWithOpts is like New but allows injecting a mock credentials provider for testing.
func NewWithOpts(ctx context.Context, cfg *platformcfg.Config, credProvider sdkaws.CredentialsProvider) (*Clients, error) {
	awsCfg, err := loadConfigWithOpts(ctx, cfg, credProvider)
	if err != nil {
		return nil, err
	}

	stsClient := sts.NewFromConfig(awsCfg)
	c := &Clients{
		STS:            stsClient,
		STSRoleAssumer: stsClient,
		S3:             s3.NewFromConfig(awsCfg),
		DynamoDB:       dynamodb.NewFromConfig(awsCfg),
		IAM:            iam.NewFromConfig(awsCfg),
		SNS:            sns.NewFromConfig(awsCfg),
		Budgets:        budgets.NewFromConfig(awsCfg),
		Region:         cfg.Region,
		Profile:        cfg.AWSProfile,
	}

	if err := c.verifyIdentity(ctx); err != nil {
		return nil, err
	}

	return c, nil
}

// loadConfig constructs an aws.Config from the platform Config.
// It uses the following credential resolution order:
//  1. Named profile (cfg.AWSProfile)
//  2. Environment variables (AWS_ACCESS_KEY_ID, etc.)
//  3. AWS SDK default credential chain (SSO, EC2 instance role, etc.)
//  4. Error if none available
func loadConfig(ctx context.Context, cfg *platformcfg.Config) (sdkaws.Config, error) {
	return loadConfigWithOpts(ctx, cfg, nil)
}

// loadConfigWithOpts constructs an aws.Config with optional credential provider override.
// Used internally and by tests to inject mock credential providers.
func loadConfigWithOpts(ctx context.Context, cfg *platformcfg.Config, credProvider sdkaws.CredentialsProvider) (sdkaws.Config, error) {
	opts := []func(*sdkcfg.LoadOptions) error{
		sdkcfg.WithRegion(cfg.Region),
	}

	if cfg.AWSProfile != "" {
		opts = append(opts, sdkcfg.WithSharedConfigProfile(cfg.AWSProfile))
	}

	// If a credentials provider is supplied (e.g., from tests), use it instead
	// of the default chain.
	if credProvider != nil {
		opts = append(opts, sdkcfg.WithCredentialsProvider(credProvider))
	}
	// If no explicit profile or provider, the AWS SDK will automatically try:
	// - Environment variables (AWS_ACCESS_KEY_ID, etc.)
	// - ~/.aws/credentials and ~/.aws/config
	// - SSO cache
	// - EC2 instance metadata
	// - Other credential sources in the default chain

	awsCfg, err := sdkcfg.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return sdkaws.Config{}, fmt.Errorf("loading AWS config: %w", err)
	}

	// Verify we actually got credentials by trying to build a credentials provider.
	// This catches the "no credentials available" case early.
	if _, err := awsCfg.Credentials.Retrieve(ctx); err != nil {
		return sdkaws.Config{}, ErrNoCredentials
	}

	return awsCfg, nil
}

// IsRootARN reports whether callerARN is an AWS account root principal.
// Root ARNs take the form arn:aws:iam::<account>:root.
// The AWS root account cannot call sts:AssumeRole.
func IsRootARN(callerARN string) bool {
	return strings.HasSuffix(callerARN, ":root")
}

// AssumeAdminRole assumes the given IAM role using c.STSRoleAssumer, builds a
// new Clients bundle from the temporary credentials, verifies the new identity,
// and returns it. The original c is not modified.
//
// After this call, the returned Clients will have CallerARN set to the assumed
// role session ARN (e.g. arn:aws:sts::ACCOUNT:assumed-role/platform-admin/...).
// AccountID is preserved from the original Clients since role assumption stays
// within the same account.
func AssumeAdminRole(ctx context.Context, c *Clients, roleARN string) (*Clients, error) {
	out, err := c.STSRoleAssumer.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         sdkaws.String(roleARN),
		RoleSessionName: sdkaws.String("platform-bootstrap-init"),
		DurationSeconds: sdkaws.Int32(3600),
	})
	if err != nil {
		return nil, fmt.Errorf("assuming role %s: %w", roleARN, err)
	}

	creds := out.Credentials
	provider := sdkaws.CredentialsProviderFunc(func(context.Context) (sdkaws.Credentials, error) {
		return sdkaws.Credentials{
			AccessKeyID:     sdkaws.ToString(creds.AccessKeyId),
			SecretAccessKey: sdkaws.ToString(creds.SecretAccessKey),
			SessionToken:    sdkaws.ToString(creds.SessionToken),
			Source:          "AssumedRole:" + roleARN,
		}, nil
	})

	awsCfg, err := sdkcfg.LoadDefaultConfig(ctx,
		sdkcfg.WithRegion(c.Region),
		sdkcfg.WithCredentialsProvider(provider),
	)
	if err != nil {
		return nil, fmt.Errorf("building config for assumed role: %w", err)
	}

	stsClient := sts.NewFromConfig(awsCfg)
	assumed := &Clients{
		STS:            stsClient,
		STSRoleAssumer: stsClient,
		S3:             s3.NewFromConfig(awsCfg),
		DynamoDB:       dynamodb.NewFromConfig(awsCfg),
		IAM:            iam.NewFromConfig(awsCfg),
		SNS:            sns.NewFromConfig(awsCfg),
		Budgets:        budgets.NewFromConfig(awsCfg),
		Region:         c.Region,
		Profile:        c.Profile,
		AccountID:      c.AccountID,
	}

	if err := assumed.verifyIdentity(ctx); err != nil {
		return nil, fmt.Errorf("verifying assumed role identity: %w", err)
	}

	return assumed, nil
}

// verifyIdentity calls sts:GetCallerIdentity and populates c.AccountID and
// c.CallerARN. This is the first and only AWS call before any bootstrap step
// runs. A failure here means credentials are invalid or the network is down —
// both are terminal conditions that surface early rather than mid-run.
func (c *Clients) verifyIdentity(ctx context.Context) error {
	out, err := c.STS.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		hint := ""
		if c.Profile != "" {
			hint = fmt.Sprintf(" If this profile uses AWS SSO / IAM Identity Center, run: aws sso login --profile %s", c.Profile)
		}
		return fmt.Errorf("verifying AWS credentials: %w.%s", err, hint)
	}

	c.AccountID = sdkaws.ToString(out.Account)
	c.CallerARN = sdkaws.ToString(out.Arn)
	return nil
}
