package cmd

import (
	"strconv"
	"strings"
	"testing"
)

func FuzzRenderBackendHCL(f *testing.F) {
	f.Add("ffreis-tf-state-root", "ffreis-tf-locks-root", "us-east-1")
	f.Add("bucket with spaces", "table/with/slash", "eu-west-1")
	f.Add("", "", "")

	f.Fuzz(func(t *testing.T, bucket, table, region string) {
		out := renderBackendHCL(backendConfig{
			Bucket:        bucket,
			DynamoDBTable: table,
			Region:        region,
		})

		if !strings.Contains(out, "bucket         = "+strconv.Quote(bucket)) {
			t.Fatalf("rendered backend config missing quoted bucket %q in %q", bucket, out)
		}
		if !strings.Contains(out, "dynamodb_table = "+strconv.Quote(table)) {
			t.Fatalf("rendered backend config missing quoted dynamodb table %q in %q", table, out)
		}
		if !strings.Contains(out, "region         = "+strconv.Quote(region)) {
			t.Fatalf("rendered backend config missing quoted region %q in %q", region, out)
		}
		if !strings.HasSuffix(out, "\n") {
			t.Fatalf("rendered backend config must end with newline: %q", out)
		}
	})
}

func FuzzSplitAlertEmails(f *testing.F) {
	f.Add("admin@example.com")
	f.Add("admin@example.com,ops@example.com")
	f.Add("  admin@example.com , ops@example.com , ")
	f.Add(",,,")
	f.Add("")

	f.Fuzz(func(t *testing.T, raw string) {
		got := splitAlertEmails(raw)

		for _, email := range got {
			if email == "" {
				t.Fatalf("splitAlertEmails(%q) produced an empty entry: %#v", raw, got)
			}
			if strings.TrimSpace(email) != email {
				t.Fatalf("splitAlertEmails(%q) left untrimmed entry %q", raw, email)
			}
			if strings.Contains(email, ",") {
				t.Fatalf("splitAlertEmails(%q) left a comma in entry %q", raw, email)
			}
		}
		if len(got) == 0 && got != nil {
			t.Fatalf("splitAlertEmails(%q) returned an empty non-nil slice", raw)
		}
		// The first entry is what the deprecated singular field carries.
		if want := primaryAlertEmail(got); len(got) > 0 && want != got[0] {
			t.Fatalf("primaryAlertEmail(%#v) = %q, want %q", got, want, got[0])
		}
	})
}
