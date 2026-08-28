// Command alchemist is a terminal IDE for Azure Cosmos DB.
package main

import (
	"fmt"
	"os"

	"github.com/colbytimm/alchemist/cmd"
)

func main() {
	if err := cmd.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "alchemist:", err)
		os.Exit(1)
	}
}
