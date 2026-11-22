package netflix

import (
	"context"

	"netflix-household-validator/internal/models"
)

type Browser interface {
	OpenUpdatePrimaryLocation(ctx context.Context, link, traceID string) (models.BrowserResult, error)
}
