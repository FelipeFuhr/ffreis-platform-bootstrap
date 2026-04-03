package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

type stepReporter interface {
	Start(step)
	Skipped(step)
	Failure(step, time.Duration, error)
	Success(step, time.Duration)
}

type noopStepReporter struct{}

func (noopStepReporter) Start(step)                         {}
func (noopStepReporter) Skipped(step)                       {}
func (noopStepReporter) Failure(step, time.Duration, error) {}
func (noopStepReporter) Success(step, time.Duration)        {}

type terminalStepReporter struct {
	presenter *platformui.Presenter
	w         io.Writer
}

func newStepReporter(presenter *platformui.Presenter, w io.Writer) stepReporter {
	if presenter == nil || !presenter.Interactive() || w == nil {
		return noopStepReporter{}
	}
	return terminalStepReporter{presenter: presenter, w: w}
}

func (r terminalStepReporter) Start(s step) {
	writeStepLine(r.w, r.presenter.Status("running", "...", fmt.Sprintf("%s: %s", s.name, s.desc)))
}

func (r terminalStepReporter) Skipped(s step) {
	writeStepLine(r.w, r.presenter.Status("muted", "skip", fmt.Sprintf("%s skipped", s.name)))
}

func (r terminalStepReporter) Failure(s step, duration time.Duration, err error) {
	writeStepLine(r.w, r.presenter.Status("error", "fail", fmt.Sprintf("%s after %s: %v", s.name, r.presenter.Duration(duration), err)))
}

func (r terminalStepReporter) Success(s step, duration time.Duration) {
	writeStepLine(r.w, r.presenter.Status("ok", "ok", fmt.Sprintf("%s in %s", s.name, r.presenter.Duration(duration))))
}

func writeStepLine(w io.Writer, line string) {
	_, _ = io.WriteString(w, line+"\n")
}

func runSteps(ctx context.Context, dryRun bool, mode stepRunMode, sequenceName string, progressOut io.Writer, steps []step) error {
	logger := logging.FromContext(ctx)
	presenter := platformui.FromContext(ctx)
	reporter := newStepReporter(presenter, progressOut)

	var errs []error
	for _, s := range steps {
		started := time.Now()
		reportStepStart(logger, reporter, sequenceName, s)

		outcome := runStep(ctx, dryRun, s)
		if outcome.skipped {
			reportStepSkipped(logger, reporter, sequenceName, s)
			continue
		}
		if outcome.err != nil {
			wrapped := fmt.Errorf("step %s: %w", s.name, outcome.err)
			reportStepFailure(logger, reporter, sequenceName, s, time.Since(started), outcome.err, mode == stepRunStopOnError)
			if mode == stepRunStopOnError {
				return fmt.Errorf("%s: %w", sequenceName, wrapped)
			}
			errs = append(errs, wrapped)
			continue
		}

		reportStepSuccess(logger, reporter, sequenceName, s, time.Since(started))
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

func reportStepStart(logger *slog.Logger, reporter stepReporter, sequenceName string, s step) {
	logger.Info("step starting",
		"sequence", sequenceName,
		"step", s.name,
		"desc", s.desc,
	)
	reporter.Start(s)
}

func reportStepSkipped(logger *slog.Logger, reporter stepReporter, sequenceName string, s step) {
	logger.Info("dry-run: skipping",
		"sequence", sequenceName,
		"step", s.name,
	)
	reporter.Skipped(s)
}

func reportStepFailure(logger *slog.Logger, reporter stepReporter, sequenceName string, s step, duration time.Duration, err error, aborting bool) {
	message := "step failed, continuing"
	if aborting {
		message = "step failed, aborting"
	}
	logger.Error(message,
		"sequence", sequenceName,
		"step", s.name,
		"error", err,
	)
	reporter.Failure(s, duration, err)
}

func reportStepSuccess(logger *slog.Logger, reporter stepReporter, sequenceName string, s step, duration time.Duration) {
	logger.Info("step complete",
		"sequence", sequenceName,
		"step", s.name,
	)
	reporter.Success(s, duration)
}
