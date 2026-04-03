package bootstrap

import (
	"errors"
	"fmt"
	"strings"

	platformaws "github.com/ffreis/platform-bootstrap/internal/aws"
)

const errClientsRequired = "clients is required when dry-run is false"

type clientRequirement struct {
	name    string
	missing func(*platformaws.Clients) bool
}

var commonClientRequirements = []clientRequirement{
	{name: "S3", missing: func(clients *platformaws.Clients) bool { return clients.S3 == nil }},
	{name: "DynamoDB", missing: func(clients *platformaws.Clients) bool { return clients.DynamoDB == nil }},
	{name: "IAM", missing: func(clients *platformaws.Clients) bool { return clients.IAM == nil }},
	{name: "SNS", missing: func(clients *platformaws.Clients) bool { return clients.SNS == nil }},
	{name: "Budgets", missing: func(clients *platformaws.Clients) bool { return clients.Budgets == nil }},
	{name: "AccountID", missing: func(clients *platformaws.Clients) bool { return clients.AccountID == "" }},
	{name: "Region", missing: func(clients *platformaws.Clients) bool { return clients.Region == "" }},
}

var bootstrapClientRequirements = append(
	append([]clientRequirement{}, commonClientRequirements...),
	clientRequirement{name: "CallerARN", missing: func(clients *platformaws.Clients) bool { return clients.CallerARN == "" }},
)

func validateClients(clients *platformaws.Clients, requirements []clientRequirement) error {
	if clients == nil {
		return errors.New(errClientsRequired)
	}

	var missing []string
	for _, requirement := range requirements {
		if requirement.missing(clients) {
			missing = append(missing, requirement.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required AWS clients/identity: %s", strings.Join(missing, ", "))
	}
	return nil
}

func validateClientsForBootstrap(clients *platformaws.Clients) error {
	return validateClients(clients, bootstrapClientRequirements)
}

func validateClientsForNuke(clients *platformaws.Clients) error {
	return validateClients(clients, commonClientRequirements)
}
