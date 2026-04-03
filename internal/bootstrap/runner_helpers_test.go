package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
	"github.com/ffreis/platform-bootstrap/internal/logging"
)

func TestBootstrapRunnerTryPublishPublishErrorStillContinues(t *testing.T) {
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(testSink{}, nil)))

	snsMock := &okSNS{publishErr: errors.New("publish failed")}
	r := &bootstrapRunner{
		cfg: &config.Config{OrgName: "acme"},
		c: &platformaws.Clients{
			SNS:       snsMock,
			CallerARN: testCallerARN,
		},
		log:   slog.Default(),
		topic: "arn:aws:sns:us-east-1:123:topic",
	}

	r.tryPublish(ctx, platformaws.NewEvent(platformaws.EventTypeResourceCreated, "S3Bucket", "b", r.c.CallerARN))

	if snsMock.publishCalls != 1 {
		t.Errorf("publishCalls: want 1, got %d", snsMock.publishCalls)
	}
}

func TestBootstrapRunnerTryRegisterRegisterErrorStillContinues(t *testing.T) {
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(testSink{}, nil)))

	dbMock := &okDynamoDB{putItemErr: errors.New("put failed")}
	r := &bootstrapRunner{
		cfg: &config.Config{OrgName: "acme"},
		c: &platformaws.Clients{
			DynamoDB:  dbMock,
			CallerARN: testCallerARN,
		},
		log:           slog.Default(),
		tags:          platformaws.RequiredTags("acme", "dev"),
		registryTable: "registry",
	}

	r.tryRegister(ctx, ResourceTypeS3Bucket, "bucket")
}
