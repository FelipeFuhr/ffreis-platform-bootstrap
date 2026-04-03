package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestExitCodeForError(t *testing.T) {
	t.Parallel()

	if got := exitCodeForError(&ExitError{Code: exitAWSError, Err: errors.New("boom")}); got != exitAWSError {
		t.Fatalf("exitCodeForError(exit error) = %d, want %d", got, exitAWSError)
	}
	if got := exitCodeForError(errors.New("boom")); got != exitUserError {
		t.Fatalf("exitCodeForError(generic error) = %d, want %d", got, exitUserError)
	}
}

func TestExecuteCommand(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	cmd := &cobra.Command{
		Use: "test",
		RunE: func(*cobra.Command, []string) error {
			return &ExitError{Code: exitAWSError, Err: errors.New("boom")}
		},
	}

	if got := executeCommand(cmd, &stderr); got != exitAWSError {
		t.Fatalf("executeCommand() = %d, want %d", got, exitAWSError)
	}
	if !strings.Contains(stderr.String(), "error: boom") {
		t.Fatalf("stderr = %q, want boom message", stderr.String())
	}
}

func TestExecuteCommandSuccess(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	cmd := &cobra.Command{
		Use: "test",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}

	if got := executeCommand(cmd, &stderr); got != exitOK {
		t.Fatalf("executeCommand() = %d, want %d", got, exitOK)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}
