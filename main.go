// Command alchemist is a terminal IDE for Azure Cosmos DB.
package main

import (
	"fmt"
	"os"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "alchemist:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := cmd.RegisterAdapters(); err != nil {
		return err
	}
	return cmd.NewRootCmd(config.SystemKeyring()).Execute()
}
