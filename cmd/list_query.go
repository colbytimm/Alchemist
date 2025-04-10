package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/spf13/cobra"
)

func ListQueryCmd(sp *services.ServiceProvider) *cobra.Command {
	var (
		verbose bool
	)

	listQueryCmd := &cobra.Command{
		Use:   "list-query",
		Short: "List all saved queries",
		Long: `List all saved queries.

Examples:
  alchemist list-query`,
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

			savedQueries, err := ListQueriesInternal(sp.DatabaseManager)
			if err != nil {
				log.Error("Could not retrieve saved queries", "error", err)
				return
			}

			if len(savedQueries) == 0 {
				log.Info("No saved queries found")
				log.Info("Use 'alchemist save-query' to save a query")
				return
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tDATABASE\tCONTAINER\tACCOUNT\tDESCRIPTION\tCREATED\tMODIFIED")

			for i := range savedQueries {
				accountName := savedQueries[i].AccountName
				if accountName == "" {
					accountName = "-"
				}

				description := savedQueries[i].Description
				if description == "" {
					description = "-"
				} else if len(description) > 30 {
					description = description[:27] + "..."
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					savedQueries[i].Name,
					savedQueries[i].DatabaseID,
					savedQueries[i].ContainerID,
					accountName,
					description,
					savedQueries[i].DateCreated,
					savedQueries[i].DateModified,
				)
			}
			if err := w.Flush(); err != nil {
				log.Printf("Failed to flush tabwriter: %v", err)
			}
		},
	}

	listQueryCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")

	return listQueryCmd
}

func ListQueriesInternal(dbManager data.DatabaseManager) ([]data.SavedQueryOptions, error) {
	err := dbManager.OpenDatabase()
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}

	err = dbManager.EnsureSavedQueryTableExists()
	if err != nil {
		return nil, fmt.Errorf("could not ensure saved query table exists: %w", err)
	}

	savedQueries, err := dbManager.GetSavedQueries()
	if err != nil {
		return nil, fmt.Errorf("could not retrieve saved queries: %w", err)
	}

	return savedQueries, nil
}
