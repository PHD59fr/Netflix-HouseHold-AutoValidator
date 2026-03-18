package netflix

import "netflix-household-validator/internal/models"

type Browser interface {
	OpenUpdatePrimaryLocation(link, traceID string) (models.BrowserResult, error)
}
