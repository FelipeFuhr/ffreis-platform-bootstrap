package aws

import "testing"

func TestRequiredTags_Fields(t *testing.T) {
	tags := RequiredTags("acme")

	cases := []struct{ key, want string }{
		{"Project", "platform"},
		{"Layer", "bootstrap"},
		{"Owner", "acme"},
	}
	for _, tc := range cases {
		if got := tags[tc.key]; got != tc.want {
			t.Errorf("RequiredTags[%q]: want %q, got %q", tc.key, tc.want, got)
		}
	}
	if len(tags) != 3 {
		t.Errorf("RequiredTags: want 3 entries, got %d", len(tags))
	}
}

func TestRequiredTags_OwnerVaries(t *testing.T) {
	tags1 := RequiredTags("org-a")
	tags2 := RequiredTags("org-b")
	if tags1["Owner"] == tags2["Owner"] {
		t.Error("Owner should differ across different org names")
	}
}
