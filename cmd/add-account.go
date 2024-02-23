package cmd

import (
	"github.com/spf13/cobra"
)

func AddAccountCmd() *cobra.Command {
	return &cobra.Command{
		Use:                   "add-account",
		Short:                 "Add Cosmos DB account",
		Long:                  "Add Cosmos DB account",
		Args:                  cobra.ExactArgs(1),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			// id, err := verifyConnectionString(args[0])
			// if err != nil {
			// 	panic("Invalid connection string")
			// }

			println("Cosmos DB account added")
		},
	}
}
