package cmd

import (
	"testing"

	"github.com/ffreis/platform-bootstrap/internal/bootstrap"
	"github.com/ffreis/platform-bootstrap/internal/config"
)

// TestBootstrapStatusRankOrdering pins the sort order the audit report relies on:
// problems must sort ahead of healthy rows, so the first thing on screen is the
// thing that needs attention.
func TestBootstrapStatusRankOrdering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status string
		want   int
	}{
		{name: "ok sorts first", status: auditStatusOK, want: 0},
		{name: "unmanaged", status: auditStatusUnmanaged, want: 1},
		{name: "owned", status: auditStatusOwned, want: 2},
		{name: "missing", status: auditStatusMissing, want: 3},
		{name: "unrecognised status sorts last", status: "totally-unknown", want: 4},
		{name: "empty status sorts last", status: "", want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := bootstrapStatusRank(tt.status); got != tt.want {
				t.Errorf("bootstrapStatusRank(%q) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

// TestMatchesBootstrapManagedName covers the org-prefix ownership heuristic.
// A false positive here makes the audit claim an unrelated resource as
// bootstrap-managed, so the negative cases matter as much as the positive ones.
func TestMatchesBootstrapManagedName(t *testing.T) {
	setTestDeps(t, &config.Config{OrgName: "acme"}, nil, nil)

	tests := []struct {
		name         string
		resourceType string
		resourceName string
		want         bool
	}{
		{
			name:         "s3 bucket with the org prefix",
			resourceType: string(bootstrap.ResourceTypeS3Bucket),
			resourceName: "acme-terraform-state",
			want:         true,
		},
		{
			name:         "dynamodb table with the org prefix",
			resourceType: string(bootstrap.ResourceTypeDynamoDBTable),
			resourceName: "acme-bootstrap-registry",
			want:         true,
		},
		{
			name:         "sns topic with the org prefix",
			resourceType: string(bootstrap.ResourceTypeSNSTopic),
			resourceName: "acme-platform-events",
			want:         true,
		},
		{
			name:         "budget with the org prefix",
			resourceType: string(bootstrap.ResourceTypeAWSBudget),
			resourceName: "acme-monthly",
			want:         true,
		},
		{
			name:         "another org's bucket is not ours",
			resourceType: string(bootstrap.ResourceTypeS3Bucket),
			resourceName: "othercorp-terraform-state",
			want:         false,
		},
		{
			name:         "org name without the separator is not a prefix match",
			resourceType: string(bootstrap.ResourceTypeS3Bucket),
			resourceName: "acmecorp-state",
			want:         false,
		},
		{
			name:         "IAM roles are never matched by name",
			resourceType: string(bootstrap.ResourceTypeIAMRole),
			resourceName: "acme-platform-admin",
			want:         false,
		},
		{
			name:         "IAM users are never matched by name",
			resourceType: string(bootstrap.ResourceTypeIAMUser),
			resourceName: "acme-admin",
			want:         false,
		},
		{
			name:         "empty name never matches",
			resourceType: string(bootstrap.ResourceTypeS3Bucket),
			resourceName: "",
			want:         false,
		},
		{
			name:         "unknown resource type never matches",
			resourceType: "AWS::Unknown::Thing",
			resourceName: "acme-thing",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesBootstrapManagedName(tt.resourceType, tt.resourceName)
			if got != tt.want {
				t.Errorf("matchesBootstrapManagedName(%q, %q) = %v, want %v",
					tt.resourceType, tt.resourceName, got, tt.want)
			}
		})
	}
}
