package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ffreis/platform-bootstrap/internal/logging"
	platformui "github.com/ffreis/platform-bootstrap/internal/ui"
)

type step struct {
	name string
	desc string
	run  func(context.Context) error
}

type stepRunMode int

const (
	stepRunStopOnError stepRunMode = iota
	stepRunContinueOnError
)

type stepOutcome struct {
	skipped bool
	err     error
}

func runSteps(ctx context.Context, dryRun bool, mode stepRunMode, sequenceName string, steps []step) error {
	logger := logging.FromContext(ctx)
	presenter := platformui.FromContext(ctx)

	var errs []error
	for _, s := range steps {
		started := time.Now()
		reportStepStart(logger, presenter, sequenceName, s)

		outcome := runStep(ctx, dryRun, s)
		if outcome.skipped {
			reportStepSkipped(logger, presenter, sequenceName, s)
			continue
		}
		if outcome.err != nil {
			wrapped := fmt.Errorf("step %s: %w", s.name, outcome.err)
			reportStepFailure(logger, presenter, sequenceName, s, time.Since(started), outcome.err, mode == stepRunStopOnError)
			if mode == stepRunStopOnError {
				return fmt.Errorf("%s: %w", sequenceName, wrapped)
			}
			errs = append(errs, wrapped)
			continue
		}

		reportStepSuccess(logger, presenter, sequenceName, s, time.Since(started))
	}

	return joinStepErrors(sequenceName, errs)
}

func runStep(ctx context.Context, dryRun bool, s step) stepOutcome {
	if dryRun {
		return stepOutcome{skipped: true}
	}
	return stepOutcome{err: s.run(ctx)}
}

func joinStepErrors(sequenceName string, errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s completed with %d error(s): %w", sequenceName, len(errs), errors.Join(errs...))
}

func reportStepStart(logger *slog.Logger, presenter *platformui.Presenter, sequenceName string, s step) {
	logger.Info("step starting",
		"sequence", sequenceName,
		"step", s.name,
		"desc", s.desc,
	)
	if presenter.Interactive() {
		fmt.Fprintln(os.Stderr, presenter.Status("running", "...", fmt.Sprintf("%s: %s", s.name, s.desc)))
	}
}

func reportStepSkipped(logger *slog.Logger, presenter *platformui.Presenter, sequenceName string, s step) {
	logger.Info("dry-run: skipping",
		"sequence", sequenceName,
		"step", s.name,
	)
	if presenter.Interactive() {
		fmt.Fprintln(os.Stderr, presenter.Status("muted", "skip", fmt.Sprintf("%s skipped", s.name)))
	}
}

func reportStepFailure(logger *slog.Logger, presenter *platformui.Presenter, sequenceName string, s step, duration time.Duration, err error, aborting bool) {
	message := "step failed, continuing"
	if aborting {
		message = "step failed, aborting"
	}
	logger.Error(message,
		"sequence", sequenceName,
		"step", s.name,
		"error", err,
	)
	if presenter.Interactive() {
		fmt.Fprintln(os.Stderr, presenter.Status("error", "fail", fmt.Sprintf("%s after %s: %v", s.name, presenter.Duration(duration), err)))
	}
}

func reportStepSuccess(logger *slog.Logger, presenter *platformui.Presenter, sequenceName string, s step, duration time.Duration) {
	logger.Info("step complete",
		"sequence", sequenceName,
		"step", s.name,
	)
	if presenter.Interactive() {
		fmt.Fprintln(os.Stderr, presenter.Status("ok", "ok", fmt.Sprintf("%s in %s", s.name, presenter.Duration(duration))))
	}
}
