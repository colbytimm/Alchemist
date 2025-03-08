package cmd

import (
	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

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

			err := data.OpenDatabase()
			if err != nil {
				log.Error("Could not open database", "error", err)
				return
			}

			log.Debug("Attempting to delete account", "name", options.Name)

			err = data.DeleteAccountByName(options.Name)
			if err != nil {
				log.Error("Failed to delete account", "name", options.Name, "error", err)
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
