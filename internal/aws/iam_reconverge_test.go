package aws

import (
	"context"
	"errors"
	"testing"
)

// TestEnsurePlatformAdminRole_PartialFailureReconverges is the gap left by
// the existing idempotency tests: those exercise "create OK, then re-run with
// role present" but never "create OK, policy attach fails, re-run heals".
//
// The scenario this protects against: CreateRole succeeded (role exists in
// IAM), then PutRolePolicy failed (network blip, throttling, transient
// permission denial). On the next bootstrap run we must NOT silently skip
// PutRolePolicy because the role already exists. We must re-attempt every
// idempotent write so the role is always brought to the declared state.
//
// Without this guarantee a single transient failure during the first run
// leaves a role with no inline policy permanently — bootstrap returns success
// on every subsequent invocation while the role is unusable.
func TestEnsurePlatformAdminRolePartialFailureReconverges(t *testing.T) {
	m := &mockIAM{
		putPolicyErr: errors.New("ThrottlingException: rate exceeded"),
	}

	// First run: CreateRole succeeds, PutRolePolicy fails. Function must
	// surface the policy error so the caller knows the run was incomplete.
	err := EnsurePlatformAdminRole(context.Background(), m, testRoleName, "123456789012", nil)
	if err == nil {
		t.Fatal("first run: expected PutRolePolicy error to surface, got nil")
	}
	if m.createRoleCalls != 1 {
		t.Errorf("first run: createRoleCalls = %d, want 1", m.createRoleCalls)
	}
	if m.putPolicyCalls != 1 {
		t.Errorf("first run: putPolicyCalls = %d, want 1 (attempted)", m.putPolicyCalls)
	}
	// Role now exists in mock state even though policy attach failed.
	if !m.roleExists {
		t.Fatal("first run: roleExists = false, expected true (CreateRole succeeded before PutRolePolicy failed)")
	}

	// Clear the transient error so the second run can succeed.
	m.putPolicyErr = nil

	// Second run: role exists, so CreateRole is skipped. But PutRolePolicy
	// MUST still be called so the previously-failed policy attach reconverges.
	if err := EnsurePlatformAdminRole(context.Background(), m, testRoleName, "123456789012", nil); err != nil {
		t.Fatalf("second run after transient error cleared: %v", err)
	}
	if m.createRoleCalls != 1 {
		t.Errorf("second run: createRoleCalls = %d, want 1 (no new create)", m.createRoleCalls)
	}
	if m.putPolicyCalls != 2 {
		t.Errorf("second run: putPolicyCalls = %d, want 2 (retry must happen)", m.putPolicyCalls)
	}
	if m.updateTrustCalls != 2 {
		t.Errorf("second run: updateTrustCalls = %d, want 2 (always reapplied)", m.updateTrustCalls)
	}
}

// TestEnsurePlatformAdminRole_TrustPolicyTransientFailureReconverges covers
// the same convergence guarantee for the trust-policy write. If a transient
// failure leaves the trust policy in a drifted state, a re-run must repair it.
func TestEnsurePlatformAdminRoleTrustPolicyTransientFailureReconverges(t *testing.T) {
	m := &mockIAM{
		roleExists:     true, // role already created in a previous bootstrap
		updateTrustErr: errors.New("ThrottlingException: rate exceeded"),
	}

	if err := EnsurePlatformAdminRole(context.Background(), m, testRoleName, "123456789012", nil); err == nil {
		t.Fatal("first run: expected UpdateAssumeRolePolicy error to surface, got nil")
	}
	if m.updateTrustCalls != 1 {
		t.Errorf("first run: updateTrustCalls = %d, want 1 (attempted)", m.updateTrustCalls)
	}
	// PutRolePolicy must NOT have run because updateTrustPolicy failed first.
	if m.putPolicyCalls != 0 {
		t.Errorf("first run: putPolicyCalls = %d, want 0 (should short-circuit on trust error)", m.putPolicyCalls)
	}

	// Clear the transient error; second run must heal the drift.
	m.updateTrustErr = nil
	if err := EnsurePlatformAdminRole(context.Background(), m, testRoleName, "123456789012", nil); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if m.updateTrustCalls != 2 {
		t.Errorf("second run: updateTrustCalls = %d, want 2 (retry)", m.updateTrustCalls)
	}
	if m.putPolicyCalls != 1 {
		t.Errorf("second run: putPolicyCalls = %d, want 1 (must run after trust succeeds)", m.putPolicyCalls)
	}
}
