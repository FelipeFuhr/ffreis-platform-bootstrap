package bootstrap

import (
	"fmt"
	"strings"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
)

func validateClientsForBootstrap(clients *platformaws.Clients) error {
	if clients == nil {
		return fmt.Errorf("clients is required when dry-run is false")
	}

	var missing []string
	if clients.S3 == nil {
		missing = append(missing, "S3")
	}
	if clients.DynamoDB == nil {
		missing = append(missing, "DynamoDB")
	}
	if clients.IAM == nil {
		missing = append(missing, "IAM")
	}
	if clients.SNS == nil {
		missing = append(missing, "SNS")
	}
	if clients.Budgets == nil {
		missing = append(missing, "Budgets")
	}
	if clients.AccountID == "" {
		missing = append(missing, "AccountID")
	}
	if clients.CallerARN == "" {
		missing = append(missing, "CallerARN")
	}
	if clients.Region == "" {
		missing = append(missing, "Region")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required AWS clients/identity: %s", strings.Join(missing, ", "))
	}

	return nil
}

func validateClientsForNuke(clients *platformaws.Clients) error {
	if clients == nil {
		return fmt.Errorf("clients is required when dry-run is false")
	}

	var missing []string
	if clients.S3 == nil {
		missing = append(missing, "S3")
	}
	if clients.DynamoDB == nil {
		missing = append(missing, "DynamoDB")
	}
	if clients.IAM == nil {
		missing = append(missing, "IAM")
	}
	if clients.SNS == nil {
		missing = append(missing, "SNS")
	}
	if clients.Budgets == nil {
		missing = append(missing, "Budgets")
	}
	if clients.AccountID == "" {
		missing = append(missing, "AccountID")
	}
	if clients.Region == "" {
		missing = append(missing, "Region")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required AWS clients/identity: %s", strings.Join(missing, ", "))
	}

	return nil
}
