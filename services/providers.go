package services

import (
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
)

// ServiceProvider manages all application dependencies.
type ServiceProvider struct {
	DatabaseManager data.DatabaseManager
	CosmosManager   cosmos.CosmosManager
}

// NewServiceProvider creates a new service provider with default implementations.
func NewServiceProvider() *ServiceProvider {
	return &ServiceProvider{
		DatabaseManager: data.NewSQLiteManager(""),
		CosmosManager:   cosmos.NewDefaultCosmosManager(),
	}
}
