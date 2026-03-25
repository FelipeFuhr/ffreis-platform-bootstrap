package aws

import "time"

// Event type constants. Values are stable identifiers — never rename them
// once they have been published, as consumers may depend on the string value.
const (
	EventTypeResourceCreated = "resource_created"
	EventTypeResourceExists  = "resource_exists"  // emitted on idempotent skip
	EventTypeResourceEnsured = "resource_ensured" // existence unknown (e.g., permission error)
)

// Event is the common payload published to the platform SNS topic.
// All fields are required; zero values are not valid for production publishes.
type Event struct {
	// EventType identifies what happened. Use the EventType* constants.
	EventType string `json:"event_type"`

	// ResourceType is the AWS resource category, e.g. "S3Bucket", "DynamoDBTable".
	ResourceType string `json:"resource_type"`

	// ResourceName is the resource identifier within its type, e.g. bucket name,
	// table name, or role name. Not an ARN — ARNs are not always available at
	// publish time and would couple the event schema to AWS internals.
	ResourceName string `json:"resource_name"`

	// Timestamp is the moment the event was generated, in UTC.
	Timestamp time.Time `json:"timestamp"`

	// Actor is the IAM principal that triggered the event (CallerARN from STS).
	Actor string `json:"actor"`
}

// NewEvent constructs an Event with Timestamp set to now (UTC).
func NewEvent(eventType, resourceType, resourceName, actor string) Event {
	return Event{
		EventType:    eventType,
		ResourceType: resourceType,
		ResourceName: resourceName,
		Timestamp:    time.Now().UTC(),
		Actor:        actor,
	}
}
