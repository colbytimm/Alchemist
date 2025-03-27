package cmd

import (
	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

func DeleteQueryCmd() *cobra.Command {
	var (
		queryName string
		verbose   bool
	)

	deleteQueryCmd := &cobra.Command{
		Use:   "delete-query",
		Short: "Delete a saved query",
		Long: `Delete a saved query by name.

Examples:
  alchemist delete-query --name "my-query"`,
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

			if queryName == "" {
				log.Error("Query name is required")
				log.Info("Use --name to specify the name of the query to delete")
				return
			}

			dbManager := data.GetDefaultManager()

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

	deleteQueryCmd.Flags().StringVarP(&queryName, "name", "n", "", "Name of the query to delete")
	deleteQueryCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")

	_ = deleteQueryCmd.MarkFlagRequired("name")

	return deleteQueryCmd
}
