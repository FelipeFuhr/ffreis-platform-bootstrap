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
		logger.Info(sequenceName+" step: "+s.desc, "step", s.name)

		if dryRun {
			logger.Info("dry-run: skipping", "step", s.name)
			continue
		}

		if err := s.run(ctx); err != nil {
			wrapped := fmt.Errorf("step %s: %w", s.name, err)
			if mode == stepRunStopOnError {
				logger.Error("step failed, aborting",
					"step", s.name,
					"error", err,
				)
				return wrapped
			}

			logger.Error("step failed, continuing",
				"step", s.name,
				"error", err,
			)
			errs = append(errs, wrapped)
			continue
		}

		logger.Info(sequenceName+" step complete", "step", s.name)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s completed with %d error(s): %w", sequenceName, len(errs), errors.Join(errs...))
	}

	return nil
}
