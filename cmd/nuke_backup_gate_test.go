package cmd

import (
	"context"
	"errors"
	"testing"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
)

// stubBackupFn swaps the state-backup implementation for the duration of a test
// and reports whether it was invoked. Restoring it is what keeps these tests
// from leaking into the rest of the cmd package.
func stubBackupFn(t *testing.T, err error) *bool {
	t.Helper()

	called := false
	old := backupBootstrapStateStoresForNukeFn
	t.Cleanup(func() { backupBootstrapStateStoresForNukeFn = old })

	backupBootstrapStateStoresForNukeFn = func(context.Context, *config.Config, *platformaws.Clients, string, bootstrapStateBackupPlan) error {
		called = true
		return err
	}
	return &called
}

// planWithData returns a plan that hasData() reports true for.
func planWithData() bootstrapStateBackupPlan {
	return bootstrapStateBackupPlan{
		StateBucket:        "acme-terraform-state",
		StateBucketObjects: 3,
		RegistryTable:      "acme-bootstrap-registry",
		RegistryTableItems: 7,
	}
}

// TestBackupBootstrapNukeAllStateSkipsWhenNothingToBackUp verifies the no-op
// path: with an empty plan the backup implementation is never invoked, so a
// nuke against an already-empty account does not write an empty backup dir.
func TestBackupBootstrapNukeAllStateSkipsWhenNothingToBackUp(t *testing.T) {
	setTestDeps(t, &config.Config{OrgName: "acme"}, nil, nil)
	called := stubBackupFn(t, nil)

	err := backupBootstrapNukeAllStateIfNeeded(
		context.Background(), &commandOutput{}, t.TempDir(), bootstrapStateBackupPlan{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *called {
		t.Error("backup ran for an empty plan; want it skipped")
	}
}

// TestBackupBootstrapNukeAllStateSkipsOnDryRun verifies --dry-run writes
// nothing. A dry run that still produced a backup would be a side effect the
// flag explicitly promises not to have.
func TestBackupBootstrapNukeAllStateSkipsOnDryRun(t *testing.T) {
	setTestDeps(t, &config.Config{OrgName: "acme", DryRun: true}, nil, nil)
	called := stubBackupFn(t, nil)

	err := backupBootstrapNukeAllStateIfNeeded(
		context.Background(), &commandOutput{}, t.TempDir(), planWithData())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *called {
		t.Error("backup ran during a dry run; want it skipped")
	}
}

// TestBackupBootstrapNukeAllStateRunsWhenPlanHasData verifies the happy path
// actually delegates to the backup implementation.
func TestBackupBootstrapNukeAllStateRunsWhenPlanHasData(t *testing.T) {
	setTestDeps(t, &config.Config{OrgName: "acme"}, nil, nil)
	called := stubBackupFn(t, nil)

	err := backupBootstrapNukeAllStateIfNeeded(
		context.Background(), &commandOutput{}, t.TempDir(), planWithData())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !*called {
		t.Error("backup did not run for a plan with data")
	}
}

// TestBackupBootstrapNukeAllStateFailureHaltsTheNuke is the important one: if
// the backup fails, the nuke must NOT proceed to delete the very state that
// failed to be backed up. The error carries exitPartialComplete so the caller
// stops rather than treating it as a soft warning.
func TestBackupBootstrapNukeAllStateFailureHaltsTheNuke(t *testing.T) {
	setTestDeps(t, &config.Config{OrgName: "acme"}, nil, nil)
	cause := errors.New("s3: AccessDenied")
	stubBackupFn(t, cause)

	err := backupBootstrapNukeAllStateIfNeeded(
		context.Background(), &commandOutput{}, t.TempDir(), planWithData())
	if err == nil {
		t.Fatal("backup failure must be reported, got nil — the nuke would proceed and destroy un-backed-up state")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T (%v), want *ExitError so the exit code propagates", err, err)
	}
	if exitErr.Code != exitPartialComplete {
		t.Errorf("exit code = %d, want exitPartialComplete (%d)", exitErr.Code, exitPartialComplete)
	}
	if !errors.Is(err, cause) {
		t.Errorf("error = %v, want it to wrap the underlying cause", err)
	}
}
