package bootstrap

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/budgets"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
)

func TestValidateClientsForBootstrapSuccess(t *testing.T) {
	var typedNilS3 *s3.Client
	var typedNilDB *dynamodb.Client
	var typedNilIAM *iam.Client
	var typedNilSNS *sns.Client
	var typedNilBudgets *budgets.Client

	clients := &platformaws.Clients{
		S3:        typedNilS3,
		DynamoDB:  typedNilDB,
		IAM:       typedNilIAM,
		SNS:       typedNilSNS,
		Budgets:   typedNilBudgets,
		AccountID: "123",
		CallerARN: testCallerARN,
		Region:    testRegion,
	}

	if err := validateClientsForBootstrap(clients); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}
}

func TestValidateClientsForBootstrapMissingFields(t *testing.T) {
	err := validateClientsForBootstrap(&platformaws.Clients{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestValidateClientsForNukeSuccess(t *testing.T) {
	var typedNilS3 *s3.Client
	var typedNilDB *dynamodb.Client
	var typedNilIAM *iam.Client
	var typedNilSNS *sns.Client
	var typedNilBudgets *budgets.Client

	clients := &platformaws.Clients{
		S3:        typedNilS3,
		DynamoDB:  typedNilDB,
		IAM:       typedNilIAM,
		SNS:       typedNilSNS,
		Budgets:   typedNilBudgets,
		AccountID: "123",
		Region:    testRegion,
	}

	if err := validateClientsForNuke(clients); err != nil {
		t.Fatalf(errUnexpectedFmt, err)
	}
}

func TestValidateClientsForNukeMissingFields(t *testing.T) {
	err := validateClientsForNuke(&platformaws.Clients{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestValidateClientsSharedRequirements(t *testing.T) {
	err := validateClients(&platformaws.Clients{}, []clientRequirement{
		{name: "AccountID", missing: func(clients *platformaws.Clients) bool { return clients.AccountID == "" }},
		{name: "CallerARN", missing: func(clients *platformaws.Clients) bool { return clients.CallerARN == "" }},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "missing required AWS clients/identity: AccountID, CallerARN" {
		t.Fatalf("validateClients() unexpected error: %v", err)
	}
}
