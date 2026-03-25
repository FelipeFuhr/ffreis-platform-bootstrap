package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunSteps_DryRunSkipsAllSteps(t *testing.T) {
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

func TestRunSteps_StopOnErrorAborts(t *testing.T) {
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

func TestRunSteps_ContinueOnErrorRunsAllStepsAndJoinsErrors(t *testing.T) {
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
