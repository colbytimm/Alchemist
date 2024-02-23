package cmd

import (
	"fmt"
	"strings"

	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

func QueryAccountCmd() *cobra.Command {
	queryAccountCmd := &cobra.Command{
		Use:                   "query-account",
		Short:                 "Query Cosmos DB accounts",
		Long:                  "Query Cosmos DB accounts",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			data.OpenDatabase()
			accounts := data.GetAccounts()
			account := accounts[0]
			cosmos.Connect(account.ConnectionString)
			databaseIds := cosmos.GetDatabaseIds()

			concatenatedIds := strings.Join(databaseIds, " ")
			fmt.Printf("databaseIds: %s", concatenatedIds)
		},
	}

	return queryAccountCmd
}
