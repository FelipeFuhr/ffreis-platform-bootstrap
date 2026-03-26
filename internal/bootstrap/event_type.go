package bootstrap

import platformaws "github.com/ffreis/platform-bootstrap/internal/aws"

func eventTypeForExistence(existed *bool) string {
	if existed == nil {
		return platformaws.EventTypeResourceEnsured
	}
	if *existed {
		return platformaws.EventTypeResourceExists
	}
	return platformaws.EventTypeResourceCreated
}
