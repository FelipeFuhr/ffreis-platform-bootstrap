package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sts"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
)

// countingRoleAssumer implements platformaws.AssumeRoler and records how
// many times AssumeRole was called, so tests can assert an escalation
// attempt was (or was not) made.
type countingRoleAssumer struct {
	calls int
	err   error
	out   *sts.AssumeRoleOutput
}

func (m *countingRoleAssumer) AssumeRole(_ context.Context, _ *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return m.out, nil
}

// TestMaybeAssumeAdminRoleSkipEscalationTrue verifies that
// cfg.SkipAdminEscalation = true skips the assume-role attempt entirely and
// returns no error — even though the underlying AssumeRole call would have
// failed had it been attempted. This is the new opt-out behavior.
func TestMaybeAssumeAdminRoleSkipEscalationTrue(t *testing.T) {
	cfg := testConfig()
	cfg.SkipAdminEscalation = true

	assumer := &countingRoleAssumer{err: errors.New("AccessDenied: not authorized to perform: sts:AssumeRole")}
	clients := &platformaws.Clients{
		STSRoleAssumer: assumer,
		AccountID:      "123456789012",
		CallerARN:      testBootstrapRoleARN, // narrow role, not platform-admin
		Region:         "us-east-1",
	}

	got, err := maybeAssumeAdminRole(context.Background(), testLogger(), cfg, clients)
	if err != nil {
		t.Fatalf("maybeAssumeAdminRole() unexpected error with skip_admin_escalation=true: %v", err)
	}
	if got != clients {
		t.Fatal("maybeAssumeAdminRole() returned different clients when skipping escalation; want the caller's identity unchanged")
	}
	if assumer.calls != 0 {
		t.Fatalf("AssumeRole called %d times; want 0 when skip_admin_escalation is set", assumer.calls)
	}
}

// TestMaybeAssumeAdminRoleDefaultAttemptsAndFailsUnchanged locks in that,
// with SkipAdminEscalation left at its zero value (false) — i.e. every
// existing caller who has not opted in — the escalation attempt still
// happens and a failure still surfaces as an *ExitError, exactly as before
// this change.
func TestMaybeAssumeAdminRoleDefaultAttemptsAndFailsUnchanged(t *testing.T) {
	cfg := testConfig()
	// cfg.SkipAdminEscalation intentionally left unset (false, its zero value).

	assumer := &countingRoleAssumer{err: errors.New("AccessDenied: not authorized to perform: sts:AssumeRole")}
	clients := &platformaws.Clients{
		STSRoleAssumer: assumer,
		AccountID:      "123456789012",
		CallerARN:      testBootstrapRoleARN,
		Region:         "us-east-1",
	}

	_, err := maybeAssumeAdminRole(context.Background(), testLogger(), cfg, clients)
	if err == nil {
		t.Fatal("maybeAssumeAdminRole() expected error when escalation fails and skip_admin_escalation is false")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitAWSError {
		t.Fatalf("maybeAssumeAdminRole() error = %v; want *ExitError{Code: exitAWSError}", err)
	}
	if !strings.Contains(err.Error(), "failed to assume platform-admin role") {
		t.Fatalf("maybeAssumeAdminRole() error text = %q; want it to mention the failed assumption", err.Error())
	}
	if assumer.calls != 1 {
		t.Fatalf("AssumeRole called %d times; want 1 (default behavior still attempts escalation)", assumer.calls)
	}
}

// TestMaybeAssumeAdminRoleAlreadyPlatformAdminSkipsAttempt locks in the
// pre-existing "already using platform-admin" short-circuit: no assume-role
// attempt is made when the caller ARN already matches, independent of the
// new flag.
func TestMaybeAssumeAdminRoleAlreadyPlatformAdminSkipsAttempt(t *testing.T) {
	cfg := testConfig()

	assumer := &countingRoleAssumer{err: errors.New("should not be called")}
	clients := &platformaws.Clients{
		STSRoleAssumer: assumer,
		AccountID:      "123456789012",
		CallerARN:      "arn:aws:sts::123456789012:role/platform-admin/session",
		Region:         "us-east-1",
	}

	got, err := maybeAssumeAdminRole(context.Background(), testLogger(), cfg, clients)
	if err != nil {
		t.Fatalf("maybeAssumeAdminRole() unexpected error: %v", err)
	}
	if got != clients {
		t.Fatal("maybeAssumeAdminRole() returned different clients when already using platform-admin")
	}
	if assumer.calls != 0 {
		t.Fatalf("AssumeRole called %d times; want 0 when caller already uses platform-admin", assumer.calls)
	}
}

// TestMaybeAssumeAdminRoleRootCredentialsHintUnchanged locks in the
// pre-existing root-credentials error message when escalation fails and the
// caller is the account root principal.
func TestMaybeAssumeAdminRoleRootCredentialsHintUnchanged(t *testing.T) {
	cfg := testConfig()

	assumer := &countingRoleAssumer{err: errors.New("root cannot assume role")}
	clients := &platformaws.Clients{
		STSRoleAssumer: assumer,
		AccountID:      "123456789012",
		CallerARN:      "arn:aws:iam::123456789012:root",
		Region:         "us-east-1",
	}

	_, err := maybeAssumeAdminRole(context.Background(), testLogger(), cfg, clients)
	if err == nil {
		t.Fatal("maybeAssumeAdminRole() expected error for root credentials")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitAWSError {
		t.Fatalf("maybeAssumeAdminRole() error = %v; want *ExitError{Code: exitAWSError}", err)
	}
	if !strings.Contains(err.Error(), "cannot use root credentials with bootstrap") {
		t.Fatalf("maybeAssumeAdminRole() error text = %q; want the root-specific hint", err.Error())
	}
}
