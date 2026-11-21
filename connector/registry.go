package connector

import (
	"context"
	"sync"

	"gorm.io/gorm"
)

// Connector defines the service-specific integration interface for a source
// (e.g. "misskey", "discord", "slack").
type Connector interface {
	// Source returns the identifier stored in internal.Action.Source.
	Source() string
	// Models returns GORM model pointers that should be auto-migrated for this connector.
	Models() []any
	// GetActionDetail loads the service-specific action metadata for actionId.
	GetActionDetail(db *gorm.DB, actionId uint) (any, error)
	// Reply sends an answer back to the originating service for actionId.
	Reply(ctx context.Context, db *gorm.DB, actionId uint, text string) error
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Connector{}
)

// Register registers a Connector under its Source() key.
func Register(c Connector) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[c.Source()] = c
}

// Get looks up the Connector registered for source.
func Get(source string) (Connector, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	c, ok := registry[source]
	return c, ok
}

// AllModels collects all GORM models from every registered Connector.
func AllModels() []any {
	registryMu.RLock()
	defer registryMu.RUnlock()
	var models []any
	for _, c := range registry {
		models = append(models, c.Models()...)
	}
	return models
}
