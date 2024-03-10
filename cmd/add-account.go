package cmd

import (
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

func AddAccountCmd() *cobra.Command {
	var options data.AccountOptions

	addAccountCmd := &cobra.Command{
		Use:                   "add-account",
		Short:                 "Add Cosmos DB account",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			data.OpenDatabase()
			data.CreateAccountTable()
			data.InsertAccount(&options)
		},
	}
	addAccountCmd.Flags().StringVarP(&options.Name, "name", "n", "", "Account name")
	addAccountCmd.MarkFlagRequired("name")
	addAccountCmd.Flags().StringVarP(&options.ConnectionString, "connection", "c", "", "Account connection string")
	addAccountCmd.MarkFlagRequired("connection")
	addAccountCmd.Flags().StringVarP(&options.Tag, "tag", "t", "", "Account tag (e.g. dev, QA, etc.)")
	addAccountCmd.MarkFlagRequired("tag")

	return addAccountCmd
}
