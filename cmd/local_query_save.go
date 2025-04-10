package cmd

import (
	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/spf13/cobra"
)

func LocalQuerySaveCmd(sp *services.ServiceProvider) *cobra.Command {
	var (
		queryName   string
		queryString string
		accountName string
		description string
		verbose     bool
	)

	cmd := &cobra.Command{
		Use:   "query save",
		Short: "Save a query for later use",
		Long: `Save a SQL query with a name for later use.

Examples:
  alchemist local query save --name "my-query" --query "SELECT * FROM database.container as c WHERE c.id = '123'"
  alchemist local query save --name "my-query" --query "SELECT * FROM database.container as c" --account "dev-account"`,
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
				log.Info("Use --name to specify a name for the saved query")
				return
			}

			if queryString == "" {
				log.Error("Query string is required")
				log.Info("Use --query to specify the query to save")
				return
			}

			err := ValidateQueryFunc(queryString)
			if err != nil {
				log.Error("Invalid query", "error", err)
				return
			}

			queryOptions, err := ExtractAndModifyQuery(queryString)
			if err != nil {
				log.Error("Failed to parse query", "error", err)
				return
			}

			dbManager := sp.DatabaseManager

			err = dbManager.OpenDatabase()
			if err != nil {
				log.Error("Could not open database", "error", err)
				return
			}

			err = dbManager.EnsureSavedQueryTableExists()
			if err != nil {
				log.Error("Could not ensure saved query table exists", "error", err)
				return
			}

			savedQueryOptions := &data.SavedQueryOptions{
				Name:        queryName,
				QueryString: queryString,
				DatabaseID:  queryOptions.DatabaseID,
				ContainerID: queryOptions.ContainerID,
				AccountName: accountName,
				Description: description,
			}

			savedQuery, err := dbManager.SaveQuery(savedQueryOptions)
			if err != nil {
				log.Error("Failed to save query", "error", err)
				return
			}

			log.Info("Query saved successfully", "name", savedQuery.Name)
			log.Info("Database", "id", savedQuery.DatabaseID)
			log.Info("Container", "id", savedQuery.ContainerID)
			if accountName != "" {
				log.Info("Account", "name", savedQuery.AccountName)
			}
			if description != "" {
				log.Info("Description", "text", savedQuery.Description)
			}
		},
	}

	cmd.Flags().StringVarP(&queryName, "name", "n", "", "Name for the saved query")
	cmd.Flags().StringVarP(&queryString, "query", "q", "", "The query to save")
	cmd.Flags().StringVarP(&accountName, "account", "a", "", "Account to use for this query")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Description for the query")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show verbose output")

	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("query")

	return cmd
}
