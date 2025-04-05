package services

import (
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
)

// NewMockServiceProvider creates a service provider with mock implementations for testing.
func NewMockServiceProvider() *ServiceProvider {
	return &ServiceProvider{
		DatabaseManager: data.NewMockDatabaseManager(),
		CosmosManager:   cosmos.NewMockCosmosManager(),
	}
}
