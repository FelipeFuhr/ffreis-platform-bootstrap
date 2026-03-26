package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/ffreis/platform-bootstrap/internal/logging"
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

	var errs []error
	for _, s := range steps {
		logger.Info("step starting",
			"sequence", sequenceName,
			"step", s.name,
			"desc", s.desc,
		)

		if dryRun {
			logger.Info("dry-run: skipping",
				"sequence", sequenceName,
				"step", s.name,
			)
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
				return fmt.Errorf("%s: %w", sequenceName, wrapped)
			}

			logger.Error("step failed, continuing",
				"sequence", sequenceName,
				"step", s.name,
				"error", err,
			)
			errs = append(errs, wrapped)
			continue
		}

		logger.Info("step complete",
			"sequence", sequenceName,
			"step", s.name,
		)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s completed with %d error(s): %w", sequenceName, len(errs), errors.Join(errs...))
	}

	return nil
}
