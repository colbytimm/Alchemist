package cmd

import (
	"github.com/colbytimm/alchemist/app"
	"github.com/colbytimm/alchemist/services"
	"github.com/logrusorgru/aurora/v4"
	"github.com/spf13/cobra"
)

func Root(sp *services.ServiceProvider) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "alchemist",
		Long:    "\n" + aurora.Magenta(app.Name).String() + " is a command line tool for querying Cosmos DB in your terminal",
		Version: app.Version,
	}

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddCommand(AddAccountCmd(sp))
	rootCmd.AddCommand(DeleteAccountCmd(sp))
	rootCmd.AddCommand(ListAccountCmd(sp))
	rootCmd.AddCommand(QueryAccountCmd(sp))
	rootCmd.AddCommand(AccountDetailsCmd(sp))
	rootCmd.AddCommand(BatchUploadCmd(sp))
	rootCmd.AddCommand(SaveQueryCmd(sp))
	rootCmd.AddCommand(ListQueryCmd(sp))
	rootCmd.AddCommand(DeleteQueryCmd(sp))

	return rootCmd
}
