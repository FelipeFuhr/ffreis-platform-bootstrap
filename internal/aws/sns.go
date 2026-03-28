package aws

import (
	"context"
	"encoding/json"
	"fmt"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// EnsureEventsTopic creates the SNS topic and returns its ARN.
//
// Idempotency: SNS CreateTopic is idempotent by the AWS API contract.
// Calling it with the same name always returns the same ARN — no duplicate
// topic is created. No GetTopic check is needed before calling this.
//
// tags is applied when non-empty; pass nil to skip tagging.
// A tagging failure is always fatal.
func EnsureEventsTopic(ctx context.Context, client SNSAPI, name string, tags map[string]string) (string, error) {
	out, err := client.CreateTopic(ctx, &sns.CreateTopicInput{
		Name: sdkaws.String(name),
	})
	if err != nil {
		return "", fmt.Errorf("ensuring SNS topic %s: %w", name, err)
	}
	topicARN := sdkaws.ToString(out.TopicArn)

	if len(tags) > 0 {
		if err := tagSNSTopic(ctx, client, topicARN, tags); err != nil {
			return "", err
		}
	}
	return topicARN, nil
}

// EnsureTopicBudgetPolicy sets a resource policy on the SNS topic that allows
// budgets.amazonaws.com to publish. This is required for AWS Budgets alert
// notifications to reach the topic.
//
// The policy is always overwritten (idempotent PUT via SetTopicAttributes).
// It includes both the account management statement (allow account root full
// access) and the budgets publisher statement.
func EnsureTopicBudgetPolicy(ctx context.Context, client SNSAPI, topicARN, accountID string) error {
	policy, err := buildTopicPolicy(topicARN, accountID)
	if err != nil {
		return fmt.Errorf("building SNS topic policy: %w", err)
	}
	_, err = client.SetTopicAttributes(ctx, &sns.SetTopicAttributesInput{
		TopicArn:       sdkaws.String(topicARN),
		AttributeName:  sdkaws.String("Policy"),
		AttributeValue: sdkaws.String(policy),
	})
	if err != nil {
		return fmt.Errorf("setting SNS topic policy on %s: %w", topicARN, err)
	}
	return nil
}

// buildTopicPolicy constructs the JSON resource policy for the SNS topic.
func buildTopicPolicy(topicARN, accountID string) (string, error) {
	p := policyDocument{
		Version: "2012-10-17",
		Statement: []policyStatement{
			{
				Sid:       "AllowAccountManagement",
				Effect:    "Allow",
				Principal: map[string]string{"AWS": fmt.Sprintf(IAMRootPrincipalARNFormat, accountID)},
				Action:    "SNS:*",
				Resource:  topicARN,
			},
			{
				Sid:       "AllowBudgetsToPublish",
				Effect:    "Allow",
				Principal: map[string]string{"Service": "budgets.amazonaws.com"},
				Action:    "SNS:Publish",
				Resource:  topicARN,
				Condition: map[string]map[string]string{
					"StringEquals": {"aws:SourceAccount": accountID},
				},
			},
		},
	}
	return marshalPolicy(p)
}

// PublishEvent marshals e as JSON and sends it to topicARN.
//
// Subject is set to the event type so that SNS email subscriptions and
// filtering rules can act on it without parsing the message body.
//
// Callers that want fire-and-forget semantics should wrap this call with
// the logging fallback helper below rather than calling it directly.
func PublishEvent(ctx context.Context, client SNSAPI, topicARN string, e Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshalling event %s: %w", e.EventType, err)
	}

	_, err = client.Publish(ctx, &sns.PublishInput{
		TopicArn: sdkaws.String(topicARN),
		Message:  sdkaws.String(string(body)),
		Subject:  sdkaws.String(e.EventType),
	})
	if err != nil {
		return fmt.Errorf("publishing event %s to %s: %w", e.EventType, topicARN, err)
	}
	return nil
}

// tagSNSTopic applies resource tags to the SNS topic using its ARN.
// TagResource is idempotent — re-applying the same tags is safe.
func tagSNSTopic(ctx context.Context, client SNSAPI, topicARN string, tags map[string]string) error {
	snsTags := make([]snstypes.Tag, 0, len(tags))
	for k, v := range tags {
		snsTags = append(snsTags, snstypes.Tag{
			Key:   sdkaws.String(k),
			Value: sdkaws.String(v),
		})
	}
	_, err := client.TagResource(ctx, &sns.TagResourceInput{
		ResourceArn: sdkaws.String(topicARN),
		Tags:        snsTags,
	})
	if err != nil {
		return fmt.Errorf("tagging SNS topic %s: %w", topicARN, err)
	}
	return nil
}
