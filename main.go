package main

import (
	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/services"
)

func main() {
	// Initialize service provider with dependencies
	sp := services.NewServiceProvider()

	// Create and execute root command
	rootCmd := cmd.Root(sp)
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
