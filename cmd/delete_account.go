package cmd

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

func DeleteAccountInternal(accountName string, verbose bool) error {
	dbManager := data.GetDefaultManager()

	err := dbManager.OpenDatabase()
	if err != nil {
		return fmt.Errorf("could not open database: %w", err)
	}

	if verbose {
		log.Debug("Attempting to delete account", "name", accountName)
	}

	err = dbManager.DeleteAccountByName(accountName)
	if err != nil {
		return fmt.Errorf("failed to delete account %s: %w", accountName, err)
	}

	return nil
}

func DeleteAccountCmd() *cobra.Command {
	var (
		options data.AccountOptions
		verbose bool
	)

	deleteAccountCmd := &cobra.Command{
		Use:                   "delete-account",
		Short:                 "Delete Cosmos DB account",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			log.SetReportTimestamp(false)

			if verbose {
				log.SetLevel(log.DebugLevel)
				log.Debug("Debug logging enabled")
			} else {
				log.SetLevel(log.InfoLevel)
			}

			err := DeleteAccountInternal(options.Name, verbose)
			if err != nil {
				log.Error(err.Error())
				return
			}

			log.Info("Account deleted successfully", "name", options.Name)
		},
	}
	deleteAccountCmd.Flags().StringVarP(&options.Name, "name", "n", "", "Account name")
	deleteAccountCmd.MarkFlagRequired("name")
	deleteAccountCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output with debug logging")

	return deleteAccountCmd
}
