package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
	"github.com/ffreis/platform-bootstrap/internal/config"
	"github.com/ffreis/platform-bootstrap/internal/logging"
)

func TestNukeNonDryRunSuccessRunsAllSteps(t *testing.T) {
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(testSink{}, nil)))

	cfg := &config.Config{
		OrgName:          "acme",
		Region:           testRegion,
		StateRegion:      testRegion,
		LogLevel:         "info",
		BudgetMonthlyUSD: 20.0,
		Accounts:         map[string]string{},
		DryRun:           false,
	}

	s3Mock := &okS3{}
	dbMock := &okDynamoDB{}
	iamMock := &okIAM{roleExists: true}
	snsMock := &okSNS{}
	budMock := &okBudgets{}

	clients := &platformaws.Clients{
		S3:        s3Mock,
		DynamoDB:  dbMock,
		IAM:       iamMock,
		SNS:       snsMock,
		Budgets:   budMock,
		AccountID: "123456789012",
		Region:    testRegion,
	}

	if err := Nuke(ctx, cfg, clients, io.Discard); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}

	if budMock.deleteCalls != 1 {
		t.Errorf("DeleteBudget calls: want 1, got %d", budMock.deleteCalls)
	}
	if snsMock.deleteCalls != 1 {
		t.Errorf("DeleteTopic calls: want 1, got %d", snsMock.deleteCalls)
	}
	if iamMock.deleteRoleCalls != 1 {
		t.Errorf("DeleteRole calls: want 1, got %d", iamMock.deleteRoleCalls)
	}
	if dbMock.deleteCalls != 2 {
		t.Errorf("DeleteTable calls: want 2, got %d", dbMock.deleteCalls)
	}
	if s3Mock.deleteBucketCalls != 1 {
		t.Errorf("DeleteBucket calls: want 1, got %d", s3Mock.deleteBucketCalls)
	}
}

func TestNukeContinuesOnError(t *testing.T) {
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(testSink{}, nil)))

	cfg := &config.Config{
		OrgName:          "acme",
		Region:           testRegion,
		StateRegion:      testRegion,
		LogLevel:         "info",
		BudgetMonthlyUSD: 20.0,
		Accounts:         map[string]string{},
		DryRun:           false,
	}

	s3Mock := &okS3{}
	dbMock := &okDynamoDB{}
	iamMock := &okIAM{roleExists: true}
	snsMock := &okSNS{}
	budMock := &okBudgets{deleteErr: errors.New("budget delete failed")}

	clients := &platformaws.Clients{
		S3:        s3Mock,
		DynamoDB:  dbMock,
		IAM:       iamMock,
		SNS:       snsMock,
		Budgets:   budMock,
		AccountID: "123456789012",
		Region:    testRegion,
	}

	if err := Nuke(ctx, cfg, clients, io.Discard); err == nil {
		t.Fatal("expected aggregated error, got nil")
	}

	if dbMock.deleteCalls != 2 {
		t.Errorf("DeleteTable calls: want 2, got %d", dbMock.deleteCalls)
	}
	if s3Mock.deleteBucketCalls != 1 {
		t.Errorf("DeleteBucket calls: want 1, got %d", s3Mock.deleteBucketCalls)
	}
}
