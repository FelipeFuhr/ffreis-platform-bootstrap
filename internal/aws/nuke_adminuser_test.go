package aws

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// deleteAdminUserIAM embeds mockIAM (whose DeleteUser/DeleteUserPolicy always
// succeed) and overrides just the calls DeleteAdminUser makes, so each failure
// branch can be driven independently.
type deleteAdminUserIAM struct {
	mockIAM

	listOut *iam.ListAccessKeysOutput
	listErr error

	deleteKeyErr    error
	deleteKeyInputs []*iam.DeleteAccessKeyInput

	deletePolicyErr   error
	deletePolicyCalls int

	deleteUserErr   error
	deleteUserCalls int
}

func (m *deleteAdminUserIAM) ListAccessKeys(_ context.Context, _ *iam.ListAccessKeysInput, _ ...func(*iam.Options)) (*iam.ListAccessKeysOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listOut != nil {
		return m.listOut, nil
	}
	return &iam.ListAccessKeysOutput{}, nil
}

func (m *deleteAdminUserIAM) DeleteAccessKey(_ context.Context, params *iam.DeleteAccessKeyInput, _ ...func(*iam.Options)) (*iam.DeleteAccessKeyOutput, error) {
	m.deleteKeyInputs = append(m.deleteKeyInputs, params)
	if m.deleteKeyErr != nil {
		return nil, m.deleteKeyErr
	}
	return &iam.DeleteAccessKeyOutput{}, nil
}

func (m *deleteAdminUserIAM) DeleteUserPolicy(_ context.Context, _ *iam.DeleteUserPolicyInput, _ ...func(*iam.Options)) (*iam.DeleteUserPolicyOutput, error) {
	m.deletePolicyCalls++
	if m.deletePolicyErr != nil {
		return nil, m.deletePolicyErr
	}
	return &iam.DeleteUserPolicyOutput{}, nil
}

func (m *deleteAdminUserIAM) DeleteUser(_ context.Context, _ *iam.DeleteUserInput, _ ...func(*iam.Options)) (*iam.DeleteUserOutput, error) {
	m.deleteUserCalls++
	if m.deleteUserErr != nil {
		return nil, m.deleteUserErr
	}
	return &iam.DeleteUserOutput{}, nil
}

func keysOutput(ids ...string) *iam.ListAccessKeysOutput {
	md := make([]iamtypes.AccessKeyMetadata, 0, len(ids))
	for _, id := range ids {
		md = append(md, iamtypes.AccessKeyMetadata{AccessKeyId: sdkaws.String(id)})
	}
	return &iam.ListAccessKeysOutput{AccessKeyMetadata: md}
}

// TestDeleteAdminUserDeletesEveryAccessKeyFirst verifies the ordering contract
// the function's own comment states: IAM refuses DeleteUser while any access key
// remains, so every key must be deleted before the user is.
func TestDeleteAdminUserDeletesEveryAccessKeyFirst(t *testing.T) {
	t.Parallel()

	m := &deleteAdminUserIAM{listOut: keysOutput("AKIA1", "AKIA2")}

	if err := DeleteAdminUser(context.Background(), m, "acme-admin"); err != nil {
		t.Fatalf("DeleteAdminUser() unexpected error: %v", err)
	}

	if len(m.deleteKeyInputs) != 2 {
		t.Fatalf("DeleteAccessKey calls: want 2, got %d", len(m.deleteKeyInputs))
	}
	for i, want := range []string{"AKIA1", "AKIA2"} {
		if got := sdkaws.ToString(m.deleteKeyInputs[i].AccessKeyId); got != want {
			t.Errorf("deleted key %d: got %q, want %q", i, got, want)
		}
		if got := sdkaws.ToString(m.deleteKeyInputs[i].UserName); got != "acme-admin" {
			t.Errorf("deleted key %d user: got %q, want acme-admin", i, got)
		}
	}
	if m.deletePolicyCalls != 1 {
		t.Errorf("DeleteUserPolicy calls: want 1, got %d", m.deletePolicyCalls)
	}
	if m.deleteUserCalls != 1 {
		t.Errorf("DeleteUser calls: want 1, got %d", m.deleteUserCalls)
	}
}

// TestDeleteAdminUserAbsentUserIsNotAnError verifies nuke is idempotent: a user
// that is already gone reports success and touches nothing else.
func TestDeleteAdminUserAbsentUserIsNotAnError(t *testing.T) {
	t.Parallel()

	m := &deleteAdminUserIAM{listErr: &iamtypes.NoSuchEntityException{}}

	if err := DeleteAdminUser(context.Background(), m, "acme-admin"); err != nil {
		t.Fatalf("DeleteAdminUser() on absent user: want nil, got %v", err)
	}
	if m.deleteUserCalls != 0 || m.deletePolicyCalls != 0 {
		t.Errorf("absent user should short-circuit; policy=%d user=%d",
			m.deletePolicyCalls, m.deleteUserCalls)
	}
}

// TestDeleteAdminUserToleratesAlreadyDeletedSubresources verifies that a
// NoSuchEntity on any individual delete is ignored — two concurrent nukes, or a
// half-finished earlier run, must not fail the second one.
func TestDeleteAdminUserToleratesAlreadyDeletedSubresources(t *testing.T) {
	t.Parallel()

	m := &deleteAdminUserIAM{
		listOut:         keysOutput("AKIA1"),
		deleteKeyErr:    &iamtypes.NoSuchEntityException{},
		deletePolicyErr: &iamtypes.NoSuchEntityException{},
		deleteUserErr:   &iamtypes.NoSuchEntityException{},
	}

	if err := DeleteAdminUser(context.Background(), m, "acme-admin"); err != nil {
		t.Fatalf("DeleteAdminUser() with already-deleted subresources: want nil, got %v", err)
	}
}

// TestDeleteAdminUserPropagatesRealFailures verifies that a genuine API error —
// as opposed to NoSuchEntity — aborts and is reported with context naming the
// operation, so a permissions problem is never mistaken for a clean nuke.
func TestDeleteAdminUserPropagatesRealFailures(t *testing.T) {
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
			wantInErr: "listing admin user access keys",
		},
		{
			name:      "deleting a key fails",
			mock:      &deleteAdminUserIAM{listOut: keysOutput("AKIA1"), deleteKeyErr: boom},
			wantInErr: "deleting admin user access key",
		},
		{
			name:      "deleting the inline policy fails",
			mock:      &deleteAdminUserIAM{deletePolicyErr: boom},
			wantInErr: "deleting admin user policy",
		},
		{
			name:      "deleting the user fails",
			mock:      &deleteAdminUserIAM{deleteUserErr: boom},
			wantInErr: "deleting admin user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := DeleteAdminUser(context.Background(), tt.mock, "acme-admin")
			if err == nil {
				t.Fatalf("DeleteAdminUser() want error, got nil")
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
