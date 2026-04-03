package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	platformui "github.com/ffreis/platform-bootstrap/internal/ui"
)

func TestRunStepsDryRunSkipsAllSteps(t *testing.T) {
	ctx := context.Background()

	calls := 0
	steps := []step{
		{name: "one", desc: "one", run: func(context.Context) error { calls++; return nil }},
		{name: "two", desc: "two", run: func(context.Context) error { calls++; return nil }},
	}

	if err := runSteps(ctx, true, stepRunStopOnError, "bootstrap", steps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected no step runs, got %d", calls)
	}
}

func TestRunStepsStopOnErrorAborts(t *testing.T) {
	ctx := context.Background()

	calls := 0
	steps := []step{
		{name: "one", desc: "one", run: func(context.Context) error { calls++; return nil }},
		{name: "two", desc: "two", run: func(context.Context) error { calls++; return errors.New("boom") }},
		{name: "three", desc: "three", run: func(context.Context) error { calls++; return nil }},
	}

	err := runSteps(ctx, false, stepRunStopOnError, "bootstrap", steps)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Fatalf("expected 2 step runs (abort after error), got %d", calls)
	}
	if !strings.Contains(err.Error(), "step two") {
		t.Fatalf("expected step name in error, got: %v", err)
	}
}

func TestRunStepsContinueOnErrorRunsAllStepsAndJoinsErrors(t *testing.T) {
	ctx := context.Background()

	calls := 0
	steps := []step{
		{name: "one", desc: "one", run: func(context.Context) error { calls++; return errors.New("e1") }},
		{name: "two", desc: "two", run: func(context.Context) error { calls++; return nil }},
		{name: "three", desc: "three", run: func(context.Context) error { calls++; return errors.New("e3") }},
	}

	err := runSteps(ctx, false, stepRunContinueOnError, "nuke", steps)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 3 {
		t.Fatalf("expected all steps to run, got %d", calls)
	}
	if !strings.Contains(err.Error(), "step one") || !strings.Contains(err.Error(), "step three") {
		t.Fatalf("expected joined step errors, got: %v", err)
	}
}

func TestRunStep(t *testing.T) {
	ctx := context.Background()

	if outcome := runStep(ctx, true, step{name: "dry"}); !outcome.skipped || outcome.err != nil {
		t.Fatalf("dry-run outcome = %+v, want skipped without error", outcome)
	}

	if outcome := runStep(ctx, false, step{name: "ok", run: func(context.Context) error { return nil }}); outcome.skipped || outcome.err != nil {
		t.Fatalf("success outcome = %+v, want success without skip/error", outcome)
	}

	boom := errors.New("boom")
	if outcome := runStep(ctx, false, step{name: "fail", run: func(context.Context) error { return boom }}); outcome.skipped || !errors.Is(outcome.err, boom) {
		t.Fatalf("error outcome = %+v, want wrapped boom", outcome)
	}
}

func TestJoinStepErrors(t *testing.T) {
	if err := joinStepErrors("bootstrap", nil); err != nil {
		t.Fatalf("joinStepErrors() unexpected error: %v", err)
	}

	err := joinStepErrors("bootstrap", []error{errors.New("one"), errors.New("two")})
	if err == nil {
		t.Fatal("joinStepErrors() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "bootstrap completed with 2 error(s)") {
		t.Fatalf("joinStepErrors() unexpected error: %v", err)
	}
}

func TestNewStepReporterNoopForNonInteractivePresenter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	reporter := newStepReporter(&platformui.Presenter{}, &buf)
	s := step{name: "one", desc: "first"}

	reporter.Start(s)
	reporter.Skipped(s)
	reporter.Failure(s, time.Second, errors.New("boom"))
	reporter.Success(s, time.Second)

	if buf.Len() != 0 {
		t.Fatalf("expected no reporter output, got %q", buf.String())
	}
}

func TestNewStepReporterTerminalOutput(t *testing.T) {
	t.Parallel()

	presenter, err := platformui.New("plain")
	if err != nil {
		t.Fatalf("ui.New() error: %v", err)
	}

	var buf bytes.Buffer
	reporter := newStepReporter(presenter, &buf)
	s := step{name: "one", desc: "first"}

	reporter.Start(s)
	reporter.Skipped(s)
	reporter.Failure(s, time.Second, errors.New("boom"))
	reporter.Success(s, time.Second)

	got := buf.String()
	for _, want := range []string{"one: first", "one skipped", "one after 1s: boom", "one in 1s"} {
		if !strings.Contains(got, want) {
			t.Fatalf("reporter output missing %q in:\n%s", want, got)
		}
	}
}
