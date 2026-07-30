package aws

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestDeleteTempBootstrapUserPropagatesRealFailures covers the error branches of
// the temp-user teardown. The happy path and the ignore-missing behaviour are
// covered in tempuser_test.go; what matters here is that a genuine API failure
// is NOT mistaken for "already cleaned up".
//
// This user holds long-lived root-derived credentials, so a teardown that
// reports success while the user still exists leaves a standing privilege the
// operator believes is gone.
//
// Reuses deleteAdminUserIAM (nuke_adminuser_test.go) — same package, same call
// shape — rather than defining a second near-identical mock.
func TestDeleteTempBootstrapUserPropagatesRealFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("AccessDenied: not authorized")

	tests := []struct {
		name      string
		mock      *deleteAdminUserIAM
		wantInErr string
	}{
		{
			name:      "listing keys fails",
			mock:      &deleteAdminUserIAM{listErr: boom},
			wantInErr: "listing temp user access keys",
		},
		{
			name:      "deleting a key fails",
			mock:      &deleteAdminUserIAM{listOut: keysOutput("AKIA1"), deleteKeyErr: boom},
			wantInErr: "deleting temp user access key",
		},
		{
			name:      "deleting the inline policy fails",
			mock:      &deleteAdminUserIAM{deletePolicyErr: boom},
			wantInErr: "deleting temp user policy",
		},
		{
			name:      "deleting the user fails",
			mock:      &deleteAdminUserIAM{deleteUserErr: boom},
			wantInErr: "deleting temp user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := DeleteTempBootstrapUser(context.Background(), tt.mock,
				TempUser{UserName: TempBootstrapUserName})
			if err == nil {
				t.Fatalf("DeleteTempBootstrapUser() want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantInErr)
			}
			if !errors.Is(err, boom) {
				t.Errorf("error = %v, want it to wrap the underlying cause", err)
			}
		})
	}
}
