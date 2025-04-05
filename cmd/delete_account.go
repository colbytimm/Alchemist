package cmd

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/spf13/cobra"
)

func DeleteAccountInternal(accountName string, dbManager data.DatabaseManager, verbose bool) error {
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

func DeleteAccountCmd(sp *services.ServiceProvider) *cobra.Command {
	var (
		accountName string
		verbose     bool
	)

	deleteAccountCmd := &cobra.Command{
		Use:                   "delete-account",
		Short:                 "Delete a Cosmos DB account",
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

			err := DeleteAccountInternal(accountName, sp.DatabaseManager, verbose)
			if err != nil {
				log.Error(err.Error())
				return
			}

			log.Info("Account deleted successfully", "name", accountName)
		},
	}

	deleteAccountCmd.Flags().StringVarP(&accountName, "name", "n", "", "Account name")
	if err := deleteAccountCmd.MarkFlagRequired("name"); err != nil {
		log.Fatal("Failed to mark 'name' flag as required", "error", err)
	}
	deleteAccountCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output for debug logging")

	return deleteAccountCmd
}
