package bootstrap

import (
	"context"
	"errors"
	"fmt"
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

func runSteps(ctx context.Context, dryRun bool, mode stepRunMode, sequenceName string, steps []step) error {
	logger := logging.FromContext(ctx)
	presenter := platformui.FromContext(ctx)

	var errs []error
	for _, s := range steps {
		started := time.Now()
		logger.Info("step starting",
			"sequence", sequenceName,
			"step", s.name,
			"desc", s.desc,
		)
		if presenter.Interactive() {
			fmt.Fprintln(os.Stderr, presenter.Status("running", "...", fmt.Sprintf("%s: %s", s.name, s.desc)))
		}

		if dryRun {
			logger.Info("dry-run: skipping",
				"sequence", sequenceName,
				"step", s.name,
			)
			if presenter.Interactive() {
				fmt.Fprintln(os.Stderr, presenter.Status("muted", "skip", fmt.Sprintf("%s skipped", s.name)))
			}
			continue
		}

		if err := s.run(ctx); err != nil {
			wrapped := fmt.Errorf("step %s: %w", s.name, err)
			if mode == stepRunStopOnError {
				logger.Error("step failed, aborting",
					"sequence", sequenceName,
					"step", s.name,
					"error", err,
				)
				if presenter.Interactive() {
					fmt.Fprintln(os.Stderr, presenter.Status("error", "fail", fmt.Sprintf("%s after %s: %v", s.name, presenter.Duration(time.Since(started)), err)))
				}
				return fmt.Errorf("%s: %w", sequenceName, wrapped)
			}

			logger.Error("step failed, continuing",
				"sequence", sequenceName,
				"step", s.name,
				"error", err,
			)
			if presenter.Interactive() {
				fmt.Fprintln(os.Stderr, presenter.Status("error", "fail", fmt.Sprintf("%s after %s: %v", s.name, presenter.Duration(time.Since(started)), err)))
			}
			errs = append(errs, wrapped)
			continue
		}

		logger.Info("step complete",
			"sequence", sequenceName,
			"step", s.name,
		)
		if presenter.Interactive() {
			fmt.Fprintln(os.Stderr, presenter.Status("ok", "ok", fmt.Sprintf("%s in %s", s.name, presenter.Duration(time.Since(started)))))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s completed with %d error(s): %w", sequenceName, len(errs), errors.Join(errs...))
	}

	return nil
}
