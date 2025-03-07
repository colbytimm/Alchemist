package cmd

import (
	"fmt"

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
			err := data.OpenDatabase()
			if err != nil {
				fmt.Printf("Error: Could not open database: %v\n", err)
				return
			}

			err = data.EnsureAccountTableExists()
			if err != nil {
				fmt.Printf("Error: Could not ensure account table exists: %v\n", err)
				return
			}

			account, err := data.InsertAccount(&options)
			if err != nil {
				fmt.Printf("Error: Could not insert account: %v\n", err)
				return
			}

			if account.IsDefault {
				fmt.Printf("Account '%s' added successfully and set as default\n", account.Name)
			} else {
				fmt.Printf("Account '%s' added successfully\n", account.Name)
			}
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
