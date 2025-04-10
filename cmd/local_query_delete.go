package cmd

import (
	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/services"
	"github.com/colbytimm/alchemist/util"
	"github.com/spf13/cobra"
)

func LocalQueryDeleteCmd(sp *services.ServiceProvider) *cobra.Command {
	var (
		queryName string
		verbose   bool
	)

	cmd := &cobra.Command{
		Use:   "query delete",
		Short: "Delete a saved query from local storage",
		Long: `Delete a saved query by name.

Examples:
  alchemist local query delete --name "my-query"`,
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			util.SetupLogging(verbose)

			if queryName == "" {
				log.Error("Query name is required")
				log.Info("Use --name to specify the name of the query to delete")
				return
			}

			dbManager := sp.DatabaseManager

			err := dbManager.OpenDatabase()
			if err != nil {
				log.Error("Could not open database", "error", err)
				return
			}

			err = dbManager.EnsureSavedQueryTableExists()
			if err != nil {
				log.Error("Could not ensure saved query table exists", "error", err)
				return
			}

			// Check if query exists first
			_, err = dbManager.GetSavedQueryByName(queryName)
			if err != nil {
				log.Error("Could not find query", "name", queryName, "error", err)
				return
			}

			err = dbManager.DeleteSavedQueryByName(queryName)
			if err != nil {
				log.Error("Could not delete query", "error", err)
				return
			}

			log.Info("Query deleted successfully", "name", queryName)
		},
	}

	cmd.Flags().StringVarP(&queryName, "name", "n", "", "Name of the query to delete")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")

	_ = cmd.MarkFlagRequired("name")

	return cmd
}
