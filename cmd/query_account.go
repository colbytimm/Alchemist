package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/log"

	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/colbytimm/alchemist/util"
	"github.com/spf13/cobra"
)

// CrossPartitionQueryImpl is used for dependency injection.
var CrossPartitionQueryImpl = cosmos.CrossPartitionQuery

func QueryAccountInternal(accountName, query string, listAll bool, databaseID, containerID string, dbManager data.DatabaseManager, cosmosManager cosmos.CosmosManager) (string, error) {
	err := dbManager.OpenDatabase()
	if err != nil {
		return "", fmt.Errorf("could not open database: %w", err)
	}

	err = dbManager.EnsureAccountTableExists()
	if err != nil {
		return "", fmt.Errorf("could not ensure account table exists: %w", err)
	}

	accounts, err := dbManager.GetAccounts()
	if err != nil {
		return "", fmt.Errorf("could not retrieve accounts: %w", err)
	}

	if len(accounts) == 0 {
		return "", errors.New("no accounts found")
	}

	var account data.AccountOptions
	if accountName == "" {
		// Find default account
		for _, acc := range accounts {
			if acc.IsDefault {
				account = acc
				break
			}
		}

		if account.Id == 0 {
			account = accounts[0]
		}
	} else {
		accountFound := false
		for _, acc := range accounts {
			if acc.Name == accountName {
				account = acc
				accountFound = true
				break
			}
		}

		if !accountFound {
			return "", fmt.Errorf("account not found: %s", accountName)
		}
	}

	if listAll {
		if databaseID == "" || containerID == "" {
			return "", errors.New("missing parameters. Database ID and Container ID are required")
		}

		return cosmosManager.CrossPartitionQuery(databaseID, containerID, account.ConnectionString, "", false)
	}

	if query == "" {
		return "", errors.New("missing query. Use --query parameter to specify a query")
	}

	queryOptions, err := ExtractAndModifyQuery(query)
	if err != nil {
		return "", fmt.Errorf("invalid query: %w", err)
	}

	return cosmosManager.CrossPartitionQuery(queryOptions.DatabaseID, queryOptions.ContainerID, account.ConnectionString, queryOptions.Query, false)
}

type DatabaseOptions struct {
	DatabaseID   string
	ContainerIDs []string
}

type QueryOptions struct {
	DatabaseID  string
	ContainerID string
	Query       string
}

type OutputFormat string

const (
	// JSON outputs results in pretty-printed JSON format.
	JSON OutputFormat = "json"
	// TABLE outputs results in a tabular format (when possible).
	TABLE OutputFormat = "table"
	// RAW outputs results in raw JSON format (not pretty-printed).
	RAW OutputFormat = "raw"
)

func ExtractAndModifyQuery(inputQuery string) (*QueryOptions, error) {
	if inputQuery == "" {
		return nil, errors.New("query cannot be empty, use --query flag to specify a query")
	}

	var queryOptions QueryOptions
	// Update regex to allow hyphens in database and container names.
	re := regexp.MustCompile(`SELECT\s+(.*\s+)?FROM\s+([^.]+)\.(\S+)\s+as\s+c\s*(.*)`)

	matches := re.FindStringSubmatch(inputQuery)
	// TODO: Possible improvement: Might not need a where clause.
	if len(matches) < 5 {
		return nil, errors.New("invalid query format, expected: SELECT * FROM database.container as c WHERE")
	}

	selectPart := matches[1]
	queryOptions.DatabaseID = matches[2]
	queryOptions.ContainerID = matches[3]
	modifiedQuerySuffix := matches[4]

	// TODO: Might throw an error if selectPart is empty
	if selectPart == "" {
		selectPart = "* "
	}

	if modifiedQuerySuffix != "" && modifiedQuerySuffix[:1] != " " {
		modifiedQuerySuffix = " " + modifiedQuerySuffix
	}

	queryOptions.Query = fmt.Sprintf("SELECT %sFROM c%s", selectPart, modifiedQuerySuffix)

	return &queryOptions, nil
}

// ValidateQueryFunc is a function variable that can be replaced for testing.
var ValidateQueryFunc = ValidateQuery

func ValidateQuery(query string) error {
	if query == "" {
		return errors.New("query cannot be empty")
	}

	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "SELECT") {
		return errors.New("query must start with SELECT")
	}

	if !regexp.MustCompile(`(?i)FROM\s+[\w-]+\.[\w-]+\s+as\s+c`).MatchString(query) {
		return errors.New("query must include FROM database.container as c")
	}

	return nil
}

func FormatOutput(jsonData string, format OutputFormat) (string, error) {
	switch format {
	case JSON:
		return jsonData, nil
	case RAW:
		var data interface{}
		if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
			return "", fmt.Errorf("error parsing JSON: %w", err)
		}
		rawBytes, err := json.Marshal(data)
		if err != nil {
			return "", fmt.Errorf("error formatting JSON: %w", err)
		}
		return string(rawBytes), nil
	case TABLE:
		return fmt.Sprintf("Table formatting not fully implemented yet.\n%s", jsonData), nil
	default:
		return jsonData, nil
	}
}

func QueryAccountCmd(sp *services.ServiceProvider) *cobra.Command {
	var (
		query          string
		accountName    string
		listAll        bool
		databaseID     string
		containerID    string
		outputFile     string
		saveQuery      bool
		savedQueryName string
		verbose        bool
	)

	queryAccountCmd := &cobra.Command{
		Use:   "query-account",
		Short: "Query your Cosmos DB account",
		Long: `Execute queries or list all documents in a Cosmos DB container.

Examples:
  alchemist query-account --query "SELECT * FROM database.container as c WHERE c.id = '123'"
  alchemist query-account --list-all --database mydb --container mycoll
  alchemist query-account --saved-query "my-query"`,
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			util.SetupLogging(verbose)

			if saveQuery {
				if savedQueryName == "" {
					log.Fatal("Query name is required when saving a query")
				}

				err := sp.DatabaseManager.OpenDatabase()
				if err != nil {
					log.Fatal("Error opening database", "error", err)
				}

				err = sp.DatabaseManager.EnsureSavedQueryTableExists()
				if err != nil {
					log.Fatal("Error ensuring saved query table exists", "error", err)
				}

				savedQueryOptions := &data.SavedQueryOptions{
					Name:        savedQueryName,
					QueryString: query,
					DatabaseID:  databaseID,
					ContainerID: containerID,
					AccountName: accountName,
					Description: "Saved from command line",
				}

				_, err = sp.DatabaseManager.SaveQuery(savedQueryOptions)
				if err != nil {
					log.Fatal("Error saving query", "error", err)
				}

				log.Info("Query saved successfully", "name", savedQueryName)
				return
			}

			result, err := QueryAccountInternal(accountName, query, listAll, databaseID, containerID, sp.DatabaseManager, sp.CosmosManager)
			if err != nil {
				log.Error(err.Error())
				return
			}

			if outputFile != "" {
				err = os.WriteFile(outputFile, []byte(result), 0o600)
				if err != nil {
					log.Error("Could not write to output file", "error", err)
					return
				}
				log.Info("Results written to file", "file", outputFile)
			} else {
				fmt.Println(result)
			}
		},
	}

	queryAccountCmd.Flags().StringVarP(&query, "query", "q", "", "Query to execute")
	queryAccountCmd.Flags().StringVarP(&accountName, "account", "a", "", "Account name to use")
	queryAccountCmd.Flags().BoolVarP(&listAll, "list-all", "l", false, "List all documents in container")
	queryAccountCmd.Flags().StringVarP(&databaseID, "database", "d", "", "Database ID")
	queryAccountCmd.Flags().StringVarP(&containerID, "container", "c", "", "Container ID")
	queryAccountCmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file path")
	queryAccountCmd.Flags().BoolVarP(&saveQuery, "save", "s", false, "Save query for future use")
	queryAccountCmd.Flags().StringVarP(&savedQueryName, "name", "n", "", "Name for saved query")
	queryAccountCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output for debug logging")

	return queryAccountCmd
}
