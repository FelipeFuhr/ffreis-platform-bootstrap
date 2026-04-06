package cmd

import (
	"context"
	"errors"
	"testing"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
)

func TestAuditHelperStatusAndOwnerFunctions(t *testing.T) {
	setTestDeps(t, testConfig(), &platformaws.Clients{}, nil)

	if got := bootstrapStatusRank("unmanaged"); got != 1 {
		t.Fatalf("bootstrapStatusRank(unmanaged) = %d", got)
	}
	if got := bootstrapStatusRank("owned"); got != 2 {
		t.Fatalf("bootstrapStatusRank(owned) = %d", got)
	}
	if got := bootstrapStatusRank("missing"); got != 3 {
		t.Fatalf("bootstrapStatusRank(missing) = %d", got)
	}

	for _, status := range []string{"missing", "unmanaged"} {
		if got := statusIcon(status); got == status {
			t.Fatalf("statusIcon(%q) should render a plain-text badge, got %q", status, got)
		}
	}
	if got := displayOwner("  "); got != "-" {
		t.Fatalf("displayOwner(blank) = %q", got)
	}

	if got := ownerFromTagMap(map[string]string{"ManagedBy": "terraform"}); got != "terraform" {
		t.Fatalf("ownerFromTagMap managedBy = %q", got)
	}
	if got := ownerFromTagMap(map[string]string{"Layer": "bootstrap"}); got != "bootstrap" {
		t.Fatalf("ownerFromTagMap layer = %q", got)
	}
	if got := ownerFromTagMap(map[string]string{}); got != "" {
		t.Fatalf("ownerFromTagMap empty = %q", got)
	}

	if got := topicNameFromARN(""); got != "" {
		t.Fatalf("topicNameFromARN(empty) = %q", got)
	}
	if got := stringValue[string](nil); got != "" {
		t.Fatalf("stringValue(nil) = %q", got)
	}
}

func TestDiscoverBootstrapLikeResourcesReturnsS3Error(t *testing.T) {
	setTestDeps(t, testConfig(), &platformaws.Clients{S3: &cmdS3Mock{listBucketsErr: errors.New("list boom")}}, nil)

	_, err := discoverBootstrapLikeResources(context.Background())
	if err == nil || err.Error() != "listing S3 buckets: list boom" {
		t.Fatalf("expected S3 discovery error, got %v", err)
	}
}
