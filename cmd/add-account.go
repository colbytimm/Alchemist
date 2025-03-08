package cmd

import (
	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

func AddAccountCmd() *cobra.Command {
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
			// Configure logger
			log.SetReportTimestamp(false)

			if verbose {
				log.SetLevel(log.DebugLevel)
				log.Debug("Debug logging enabled")
			} else {
				log.SetLevel(log.InfoLevel)
			}

			err := data.OpenDatabase()
			if err != nil {
				log.Error("Could not open database", "error", err)
				return
			}

			err = data.EnsureAccountTableExists()
			if err != nil {
				log.Error("Could not ensure account table exists", "error", err)
				return
			}

			account, err := data.InsertAccount(&options)
			if err != nil {
				log.Error("Could not insert account", "error", err)
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
	addAccountCmd.MarkFlagRequired("name")
	addAccountCmd.Flags().StringVarP(&options.ConnectionString, "connection", "c", "", "Account connection string")
	addAccountCmd.MarkFlagRequired("connection")
	addAccountCmd.Flags().StringVarP(&options.Tag, "tag", "t", "", "Account tag (e.g. dev, QA, etc.)")
	addAccountCmd.MarkFlagRequired("tag")
	addAccountCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output with debug logging")

	return addAccountCmd
}
