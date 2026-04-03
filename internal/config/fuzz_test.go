package config

import (
	"strings"
	"testing"
)

func FuzzSplitTrimmed(f *testing.F) {
	f.Add("dev, prod , ,stage", ",")
	f.Add("us-east-1|eu-west-1", "|")
	f.Add("", ",")

	f.Fuzz(func(t *testing.T, input, sep string) {
		if sep == "" {
			sep = ","
		}

		out := splitTrimmed(input, sep)
		for _, item := range out {
			if item == "" {
				t.Fatalf("splitTrimmed returned empty item for input %q sep %q", input, sep)
			}
			if item != strings.TrimSpace(item) {
				t.Fatalf("splitTrimmed returned untrimmed item %q for input %q sep %q", item, input, sep)
			}
		}
	})
}

func FuzzParseAccounts(f *testing.F) {
	f.Add("dev:dev@example.com,prod:prod@example.com")
	f.Add(" dev : dev@example.com ")
	f.Add("invalid")
	f.Add("")

	f.Fuzz(func(t *testing.T, raw string) {
		pairs := splitTrimmed(raw, ",")
		out, err := parseAccounts(pairs)
		if err != nil {
			return
		}

		if len(out) > len(pairs) {
			t.Fatalf("parseAccounts returned more entries than pairs: got %d want <= %d", len(out), len(pairs))
		}

		for name, email := range out {
			if name == "" || email == "" {
				t.Fatalf("parseAccounts returned empty name/email: %q=%q", name, email)
			}
			if name != strings.TrimSpace(name) || email != strings.TrimSpace(email) {
				t.Fatalf("parseAccounts returned untrimmed values: %q=%q", name, email)
			}
		}
	})
}
