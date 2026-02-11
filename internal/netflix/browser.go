package netflix

import "netflix-household-validator/internal/models"

type Browser interface {
OpenUpdatePrimaryLocation(link, email, password string, traceID string) (models.BrowserResult, error)
}
