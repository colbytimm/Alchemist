package cmd

import (
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

func DeleteAccountCmd() *cobra.Command {
	var options data.AccountOptions

	deleteAccountCmd := &cobra.Command{
		Use:                   "delete-account",
		Short:                 "Delete Cosmos DB account",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			data.OpenDatabase()
			data.DeleteAccountByName(options.Name)
		},
	}
	deleteAccountCmd.Flags().StringVarP(&options.Name, "name", "n", "", "Account name")
	deleteAccountCmd.MarkFlagRequired("name")

	return deleteAccountCmd
}
