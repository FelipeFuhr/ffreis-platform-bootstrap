package aws

import "testing"

func TestRequiredTags_Fields(t *testing.T) {
	tags := RequiredTags("acme", "v1.2.3")

	cases := []struct{ key, want string }{
		{"Project", "platform"},
		{"Layer", "bootstrap"},
		{"Owner", "acme"},
		{"ManagedBy", "platform-bootstrap"},
		{"ToolVersion", "v1.2.3"},
	}
	for _, tc := range cases {
		if got := tags[tc.key]; got != tc.want {
			t.Errorf("RequiredTags[%q]: want %q, got %q", tc.key, tc.want, got)
		}
	}
	if len(tags) != 5 {
		t.Errorf("RequiredTags: want 5 entries, got %d", len(tags))
	}
}

func TestRequiredTags_OwnerVaries(t *testing.T) {
	tags1 := RequiredTags("org-a", "dev")
	tags2 := RequiredTags("org-b", "dev")
	if tags1["Owner"] == tags2["Owner"] {
		t.Error("Owner should differ across different org names")
	}
}

func TestRequiredTags_EmptyVersionDefaultsToDev(t *testing.T) {
	tags := RequiredTags("acme", "")
	if tags["ToolVersion"] != "dev" {
		t.Errorf("empty toolVersion: want %q, got %q", "dev", tags["ToolVersion"])
	}
}
