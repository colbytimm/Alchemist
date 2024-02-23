package cmd

import (
	"fmt"

	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

func ListAccountCmd() *cobra.Command {
	addAccountCmd := &cobra.Command{
		Use:                   "list-account",
		Short:                 "List Cosmos DB accounts",
		Long:                  "List Cosmos DB accounts",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			data.OpenDatabase()
			accounts := data.GetAccounts()
			for _, account := range accounts {
				fmt.Printf("%d - %s - %s - %s\n", account.Id, account.Name, account.ConnectionString, account.Tag)
			}
		},
	}

	return addAccountCmd
}
