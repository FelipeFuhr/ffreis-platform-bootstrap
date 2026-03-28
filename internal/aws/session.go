package aws

import (
	"context"
	"errors"
	"fmt"
	"os"

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
	STS      CallerIdentityGetter
	S3       S3API
	DynamoDB DynamoDBAPI
	IAM      IAMAPI
	SNS      SNSAPI
	Budgets  BudgetsAPI

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
	awsCfg, err := loadConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	c := &Clients{
		STS:      sts.NewFromConfig(awsCfg),
		S3:       s3.NewFromConfig(awsCfg),
		DynamoDB: dynamodb.NewFromConfig(awsCfg),
		IAM:      iam.NewFromConfig(awsCfg),
		SNS:      sns.NewFromConfig(awsCfg),
		Budgets:  budgets.NewFromConfig(awsCfg),
		Region:   cfg.Region,
		Profile:  cfg.AWSProfile,
	}

	if err := c.verifyIdentity(ctx); err != nil {
		return nil, err
	}

	return c, nil
}

// loadConfig constructs an aws.Config from the platform Config.
func loadConfig(ctx context.Context, cfg *platformcfg.Config) (sdkaws.Config, error) {
	opts := []func(*sdkcfg.LoadOptions) error{
		sdkcfg.WithRegion(cfg.Region),
	}

	switch {
	case cfg.AWSProfile != "":
		opts = append(opts, sdkcfg.WithSharedConfigProfile(cfg.AWSProfile))

	case os.Getenv("AWS_ACCESS_KEY_ID") != "":
		// The SDK picks up AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY /
		// AWS_SESSION_TOKEN automatically via its default credential chain.
		// No additional option is required.

	default:
		return sdkaws.Config{}, ErrNoCredentials
	}

	awsCfg, err := sdkcfg.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return sdkaws.Config{}, fmt.Errorf("loading AWS config: %w", err)
	}

	return awsCfg, nil
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
