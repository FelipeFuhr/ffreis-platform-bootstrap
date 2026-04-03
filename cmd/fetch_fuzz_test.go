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
