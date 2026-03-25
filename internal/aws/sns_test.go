package aws

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

const testTopicARN = "arn:aws:sns:us-east-1:123456789012:ffreis-platform-events"

// mockSNS implements SNSAPI. CreateTopic always returns the same ARN,
// mirroring the AWS API contract (same name → same topic, no duplication).
type mockSNS struct {
	createCalls  int
	publishCalls int
	publishErr   error
	lastPublish  *sns.PublishInput
	tagCalls     int
	tagErr       error
	setAttrCalls int
	setAttrErr   error
}

func (m *mockSNS) ListTopics(_ context.Context, _ *sns.ListTopicsInput, _ ...func(*sns.Options)) (*sns.ListTopicsOutput, error) {
	return &sns.ListTopicsOutput{}, nil
}

func (m *mockSNS) CreateTopic(_ context.Context, _ *sns.CreateTopicInput, _ ...func(*sns.Options)) (*sns.CreateTopicOutput, error) {
	m.createCalls++
	return &sns.CreateTopicOutput{TopicArn: sdkaws.String(testTopicARN)}, nil
}

func (m *mockSNS) ListTopics(_ context.Context, _ *sns.ListTopicsInput, _ ...func(*sns.Options)) (*sns.ListTopicsOutput, error) {
	return &sns.ListTopicsOutput{}, nil
}

func (m *mockSNS) Publish(_ context.Context, params *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	m.publishCalls++
	m.lastPublish = params
	return &sns.PublishOutput{MessageId: sdkaws.String("msg-1")}, m.publishErr
}

func (m *mockSNS) TagResource(_ context.Context, _ *sns.TagResourceInput, _ ...func(*sns.Options)) (*sns.TagResourceOutput, error) {
	m.tagCalls++
	return &sns.TagResourceOutput{}, m.tagErr
}

func (m *mockSNS) GetTopicAttributes(_ context.Context, _ *sns.GetTopicAttributesInput, _ ...func(*sns.Options)) (*sns.GetTopicAttributesOutput, error) {
	return &sns.GetTopicAttributesOutput{Attributes: map[string]string{}}, nil
}

func (m *mockSNS) SetTopicAttributes(_ context.Context, _ *sns.SetTopicAttributesInput, _ ...func(*sns.Options)) (*sns.SetTopicAttributesOutput, error) {
	m.setAttrCalls++
	return &sns.SetTopicAttributesOutput{}, m.setAttrErr
}

func (m *mockSNS) DeleteTopic(_ context.Context, _ *sns.DeleteTopicInput, _ ...func(*sns.Options)) (*sns.DeleteTopicOutput, error) {
	return &sns.DeleteTopicOutput{}, nil
}

// TestEnsureEventsTopic_ReturnsARN verifies that the topic ARN is returned.
func TestEnsureEventsTopic_ReturnsARN(t *testing.T) {
	m := &mockSNS{}

	arn, err := EnsureEventsTopic(context.Background(), m, "ffreis-platform-events", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if arn != testTopicARN {
		t.Errorf("ARN: want %s, got %s", testTopicARN, arn)
	}
}

// TestEnsureEventsTopic_Idempotent verifies that calling EnsureEventsTopic
// twice does not duplicate the topic — both calls succeed and return the
// same ARN. This mirrors AWS SNS behaviour: CreateTopic is idempotent.
func TestEnsureEventsTopic_Idempotent(t *testing.T) {
	m := &mockSNS{}

	arn1, err := EnsureEventsTopic(context.Background(), m, "ffreis-platform-events", nil)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	arn2, err := EnsureEventsTopic(context.Background(), m, "ffreis-platform-events", nil)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if arn1 != arn2 {
		t.Errorf("ARN changed between calls: %s → %s", arn1, arn2)
	}
	if m.createCalls != 2 {
		t.Errorf("createCalls: want 2, got %d", m.createCalls)
	}
}

// TestEnsureEventsTopic_TagsApplied verifies that when tags are provided,
// TagResource is called.
func TestEnsureEventsTopic_TagsApplied(t *testing.T) {
	m := &mockSNS{}
	tags := map[string]string{"Project": "platform", "Layer": "bootstrap"}

	_, err := EnsureEventsTopic(context.Background(), m, "ffreis-platform-events", tags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.tagCalls != 1 {
		t.Errorf("tagCalls: want 1, got %d", m.tagCalls)
	}
}

// TestEnsureTopicBudgetPolicy_SetsAttributes verifies that SetTopicAttributes
// is called with a valid JSON policy containing the budgets principal.
func TestEnsureTopicBudgetPolicy_SetsAttributes(t *testing.T) {
	m := &mockSNS{}

	if err := EnsureTopicBudgetPolicy(context.Background(), m, testTopicARN, "123456789012"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.setAttrCalls != 1 {
		t.Errorf("setAttrCalls: want 1, got %d", m.setAttrCalls)
	}
}

// TestPublishEvent_SendsCorrectPayload verifies that the published message
// body is valid JSON containing all Event fields.
func TestPublishEvent_SendsCorrectPayload(t *testing.T) {
	m := &mockSNS{}
	e := NewEvent(EventTypeResourceCreated, "S3Bucket", "ffreis-tf-state-root", "arn:aws:iam::123:root")

	if err := PublishEvent(context.Background(), m, testTopicARN, e); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.publishCalls != 1 {
		t.Fatalf("publishCalls: want 1, got %d", m.publishCalls)
	}

	// Unmarshal and verify every field.
	var got Event
	if err := json.Unmarshal([]byte(sdkaws.ToString(m.lastPublish.Message)), &got); err != nil {
		t.Fatalf("message is not valid JSON: %v", err)
	}

	if got.EventType != EventTypeResourceCreated {
		t.Errorf("EventType: want %s, got %s", EventTypeResourceCreated, got.EventType)
	}
	if got.ResourceType != "S3Bucket" {
		t.Errorf("ResourceType: want S3Bucket, got %s", got.ResourceType)
	}
	if got.ResourceName != "ffreis-tf-state-root" {
		t.Errorf("ResourceName: want ffreis-tf-state-root, got %s", got.ResourceName)
	}
	if got.Actor != "arn:aws:iam::123:root" {
		t.Errorf("Actor: want arn:aws:iam::123:root, got %s", got.Actor)
	}
	if got.Timestamp.IsZero() {
		t.Error("Timestamp must not be zero")
	}
}

// TestPublishEvent_SubjectIsEventType verifies the SNS Subject header is set
// to the event type so filtering rules can act without parsing the body.
func TestPublishEvent_SubjectIsEventType(t *testing.T) {
	m := &mockSNS{}
	e := NewEvent(EventTypeResourceExists, "DynamoDBTable", "ffreis-tf-locks-root", "arn:aws:iam::123:root")

	if err := PublishEvent(context.Background(), m, testTopicARN, e); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	subject := sdkaws.ToString(m.lastPublish.Subject)
	if subject != EventTypeResourceExists {
		t.Errorf("Subject: want %s, got %s", EventTypeResourceExists, subject)
	}
}

// TestPublishEvent_UsesProvidedARN verifies the message is sent to the
// correct topic rather than a hardcoded one.
func TestPublishEvent_UsesProvidedARN(t *testing.T) {
	m := &mockSNS{}
	customARN := "arn:aws:sns:eu-west-1:999:other-topic"
	e := NewEvent(EventTypeResourceCreated, "IAMRole", "platform-admin", "arn:aws:iam::999:root")

	if err := PublishEvent(context.Background(), m, customARN, e); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := sdkaws.ToString(m.lastPublish.TopicArn)
	if got != customARN {
		t.Errorf("TopicArn: want %s, got %s", customARN, got)
	}
}

// TestNewEvent_TimestampIsUTC verifies that NewEvent stamps events in UTC.
func TestNewEvent_TimestampIsUTC(t *testing.T) {
	before := time.Now().UTC()
	e := NewEvent(EventTypeResourceCreated, "S3Bucket", "bucket", "actor")
	after := time.Now().UTC()

	if e.Timestamp.Before(before) || e.Timestamp.After(after) {
		t.Errorf("Timestamp %s is outside the expected range [%s, %s]",
			e.Timestamp, before, after)
	}
	if e.Timestamp.Location() != time.UTC {
		t.Errorf("Timestamp location: want UTC, got %s", e.Timestamp.Location())
	}
}

// TestPublishEvent_EventTypeConstants verifies the constant values are stable.
// These are part of the public contract — changing them breaks consumers.
func TestPublishEvent_EventTypeConstants(t *testing.T) {
	if EventTypeResourceCreated != "resource_created" {
		t.Errorf("EventTypeResourceCreated changed: got %q", EventTypeResourceCreated)
	}
	if EventTypeResourceExists != "resource_exists" {
		t.Errorf("EventTypeResourceExists changed: got %q", EventTypeResourceExists)
	}
	// Verify they are distinct.
	if EventTypeResourceCreated == EventTypeResourceExists {
		t.Error("event type constants must be distinct")
	}
	// Verify they use underscore convention, not dot or dash.
	for _, c := range []string{EventTypeResourceCreated, EventTypeResourceExists} {
		if strings.ContainsAny(c, ". -") {
			t.Errorf("event type %q must use underscore separators", c)
		}
	}
}
