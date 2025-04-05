package cmd

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/colbytimm/alchemist/util"
	"github.com/spf13/cobra"
)

func AddAccountInternal(options *data.AccountOptions, dbManager data.DatabaseManager, verbose bool) (data.AccountOptions, error) {
	err := dbManager.OpenDatabase()
	if err != nil {
		return data.AccountOptions{}, fmt.Errorf("could not open database: %w", err)
	}

	err = dbManager.EnsureAccountTableExists()
	if err != nil {
		return data.AccountOptions{}, fmt.Errorf("could not ensure account table exists: %w", err)
	}

	account, err := dbManager.InsertAccount(options)
	if err != nil {
		return data.AccountOptions{}, fmt.Errorf("could not insert account: %w", err)
	}

	return account, nil
}

func AddAccountCmd(sp *services.ServiceProvider) *cobra.Command {
	var (
		options data.AccountOptions
		verbose bool
	)

	addAccountCmd := &cobra.Command{
		Use:                   "add-account",
		Short:                 "Add Cosmos DB account",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			util.SetupLogging(verbose)

			account, err := AddAccountInternal(&options, sp.DatabaseManager, verbose)
			if err != nil {
				log.Error(err.Error())
				return
			}

			if account.IsDefault {
				log.Info("Account added successfully and set as default", "name", account.Name)
			} else {
				log.Info("Account added successfully", "name", account.Name)
			}
		},
	}
	addAccountCmd.Flags().StringVarP(&options.Name, "name", "n", "", "Account name")
	if err := addAccountCmd.MarkFlagRequired("name"); err != nil {
		log.Fatal("Failed to mark 'name' flag as required", "error", err)
	}
	addAccountCmd.Flags().StringVarP(&options.ConnectionString, "connection", "c", "", "Account connection string")
	if err := addAccountCmd.MarkFlagRequired("connection"); err != nil {
		log.Fatal("Failed to mark 'connection' flag as required", "error", err)
	}
	addAccountCmd.Flags().StringVarP(&options.Tag, "tag", "t", "", "Account tag (e.g. dev, QA, etc.)")
	if err := addAccountCmd.MarkFlagRequired("tag"); err != nil {
		log.Fatal("Failed to mark 'tag' flag as required", "error", err)
	}
	addAccountCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output for debug logging")

	return addAccountCmd
}
