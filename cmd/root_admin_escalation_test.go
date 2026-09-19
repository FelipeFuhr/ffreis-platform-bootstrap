package cmd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdkaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/spf13/cobra"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
)

// newFakeSTSServer starts an httptest server that answers GetCallerIdentity
// with a fixed assumed-role identity. AssumeAdminRole builds a fresh
// *sts.Client from a real aws.Config to verify the identity it just assumed,
// so exercising that success path (rather than only the AssumeRole call,
// which is already mockable via platformaws.AssumeRoler) requires a real STS
// endpoint to point at — same approach as
// internal/aws/session_assume_test.go's newSTSTestServer.
func newFakeSTSServer(t *testing.T, assumedArn string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		action := r.URL.Query().Get("Action")
		if action == "" {
			for _, part := range strings.Split(string(bodyBytes), "&") {
				if strings.HasPrefix(part, "Action=") {
					action = strings.TrimPrefix(part, "Action=")
				}
			}
		}
		if action != "GetCallerIdentity" {
			t.Fatalf("unexpected STS action %q", action)
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <GetCallerIdentityResult>
    <Arn>`+assumedArn+`</Arn>
    <Account>123456789012</Account>
    <UserId>AROATEST:platform-bootstrap-init</UserId>
  </GetCallerIdentityResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</GetCallerIdentityResponse>`)
	}))
	t.Cleanup(server.Close)
	return server
}

// successRoleAssumer is a platformaws.AssumeRoler that always returns valid
// temporary credentials.
type successRoleAssumer struct{ calls int }

func (m *successRoleAssumer) AssumeRole(_ context.Context, _ *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	m.calls++
	return &sts.AssumeRoleOutput{
		Credentials: &ststypes.Credentials{
			AccessKeyId:     sdkaws.String("ASIAEXAMPLE"),
			SecretAccessKey: sdkaws.String("secret"),
			SessionToken:    sdkaws.String("token"),
			Expiration:      sdkaws.Time(time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)),
		},
	}, nil
}

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

// TestMaybeAssumeAdminRoleDefaultSucceeds locks in the pre-existing success
// path: with SkipAdminEscalation false (default) and the assume-role call
// succeeding, the caller is escalated to the platform-admin identity.
func TestMaybeAssumeAdminRoleDefaultSucceeds(t *testing.T) {
	assumedArn := "arn:aws:sts::123456789012:assumed-role/platform-admin/platform-bootstrap-init"
	server := newFakeSTSServer(t, assumedArn)
	t.Setenv("AWS_ENDPOINT_URL_STS", server.URL)

	cfg := testConfig()
	assumer := &successRoleAssumer{}
	clients := &platformaws.Clients{
		STSRoleAssumer: assumer,
		AccountID:      "123456789012",
		CallerARN:      testBootstrapRoleARN,
		Region:         "us-east-1",
	}

	got, err := maybeAssumeAdminRole(context.Background(), testLogger(), cfg, clients)
	if err != nil {
		t.Fatalf("maybeAssumeAdminRole() unexpected error: %v", err)
	}
	if got.CallerARN != assumedArn {
		t.Fatalf("CallerARN = %q; want %q", got.CallerARN, assumedArn)
	}
	if assumer.calls != 1 {
		t.Fatalf("AssumeRole called %d times; want 1", assumer.calls)
	}
}

// TestRootPersistentPreRunESuccess exercises PersistentPreRunE end to end —
// config resolution, UI/logger setup, identity verification (via the
// newAWSClientsFn seam), and the platform-admin escalation call — to lock in
// that the default (skip_admin_escalation unset) path is unchanged all the
// way through, not just within the extracted maybeAssumeAdminRole helper.
func TestRootPersistentPreRunESuccess(t *testing.T) {
	assumedArn := "arn:aws:sts::123456789012:assumed-role/platform-admin/platform-bootstrap-init"
	server := newFakeSTSServer(t, assumedArn)
	t.Setenv("AWS_ENDPOINT_URL_STS", server.URL)

	oldNewClients := newAWSClientsFn
	t.Cleanup(func() { newAWSClientsFn = oldNewClients })
	assumer := &successRoleAssumer{}
	newAWSClientsFn = func(context.Context, *config.Config) (*platformaws.Clients, error) {
		return &platformaws.Clients{
			STSRoleAssumer: assumer,
			AccountID:      "123456789012",
			CallerARN:      testBootstrapRoleARN,
			Region:         "us-east-1",
		}, nil
	}

	oldDeps := deps
	t.Cleanup(func() { deps = oldDeps })

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	f := cmd.Flags()
	f.String("org", "", "")
	f.String("profile", "", "")
	f.String("region", "", "")
	f.String("log-level", "", "")
	f.Bool("dry-run", false, "")
	f.Bool("skip-admin-escalation", false, "")
	f.String("ui", "auto", "")
	for flag, value := range map[string]string{"org": "acme", "region": "us-east-1", "ui": "plain"} {
		if err := f.Set(flag, value); err != nil {
			t.Fatalf("Set(%s) unexpected error: %v", flag, err)
		}
	}

	if err := rootCmd.PersistentPreRunE(cmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE() unexpected error: %v", err)
	}
	if deps.clients == nil || deps.clients.CallerARN != assumedArn {
		t.Fatalf("deps.clients = %+v; want CallerARN %q", deps.clients, assumedArn)
	}
	if assumer.calls != 1 {
		t.Fatalf("AssumeRole called %d times; want 1", assumer.calls)
	}
}

// TestRootPersistentPreRunEEscalationFailurePropagates verifies that an
// escalation failure from maybeAssumeAdminRole propagates out of
// PersistentPreRunE unchanged, and that deps.clients is left untouched.
func TestRootPersistentPreRunEEscalationFailurePropagates(t *testing.T) {
	oldNewClients := newAWSClientsFn
	t.Cleanup(func() { newAWSClientsFn = oldNewClients })
	assumer := &countingRoleAssumer{err: errors.New("AccessDenied: not authorized to perform: sts:AssumeRole")}
	newAWSClientsFn = func(context.Context, *config.Config) (*platformaws.Clients, error) {
		return &platformaws.Clients{
			STSRoleAssumer: assumer,
			AccountID:      "123456789012",
			CallerARN:      testBootstrapRoleARN,
			Region:         "us-east-1",
		}, nil
	}

	oldDeps := deps
	t.Cleanup(func() { deps = oldDeps })
	deps.clients = nil

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	f := cmd.Flags()
	f.String("org", "", "")
	f.String("profile", "", "")
	f.String("region", "", "")
	f.String("log-level", "", "")
	f.Bool("dry-run", false, "")
	f.Bool("skip-admin-escalation", false, "")
	f.String("ui", "auto", "")
	for flag, value := range map[string]string{"org": "acme", "region": "us-east-1", "ui": "plain"} {
		if err := f.Set(flag, value); err != nil {
			t.Fatalf("Set(%s) unexpected error: %v", flag, err)
		}
	}

	err := rootCmd.PersistentPreRunE(cmd, nil)
	if err == nil {
		t.Fatal("PersistentPreRunE() expected error when escalation fails")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != exitAWSError {
		t.Fatalf("PersistentPreRunE() error = %v; want *ExitError{Code: exitAWSError}", err)
	}
	if deps.clients != nil {
		t.Fatalf("deps.clients = %+v; want nil after an escalation failure", deps.clients)
	}
	if assumer.calls != 1 {
		t.Fatalf("AssumeRole called %d times; want 1", assumer.calls)
	}
}
