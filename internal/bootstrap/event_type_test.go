package bootstrap

import (
	"testing"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
)

func TestEventTypeForExistence(t *testing.T) {
	if got := eventTypeForExistence(nil); got != platformaws.EventTypeResourceEnsured {
		t.Fatalf("nil existed: want %q, got %q", platformaws.EventTypeResourceEnsured, got)
	}

	existed := true
	if got := eventTypeForExistence(&existed); got != platformaws.EventTypeResourceExists {
		t.Fatalf("existed true: want %q, got %q", platformaws.EventTypeResourceExists, got)
	}

	existed = false
	if got := eventTypeForExistence(&existed); got != platformaws.EventTypeResourceCreated {
		t.Fatalf("existed false: want %q, got %q", platformaws.EventTypeResourceCreated, got)
	}
}
