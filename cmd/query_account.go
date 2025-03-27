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
	"github.com/colbytimm/alchemist/util"
	"github.com/spf13/cobra"
)

// CrossPartitionQueryImpl is used for dependency injection.
var CrossPartitionQueryImpl = cosmos.CrossPartitionQuery

func QueryAccountInternal(accountName, query string, listAll bool, databaseId, containerId string) (string, error) {
	dbManager := data.GetDefaultManager()

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
		if databaseId == "" || containerId == "" {
			return "", errors.New("missing parameters. Database ID and Container ID are required")
		}

		return CrossPartitionQueryImpl(databaseId, containerId, account.ConnectionString, "", false)
	} else {
		if query == "" {
			return "", errors.New("missing query. Use --query parameter to specify a query")
		}

		queryOptions, err := ExtractAndModifyQuery(query)
		if err != nil {
			return "", fmt.Errorf("invalid query: %w", err)
		}

		return CrossPartitionQueryImpl(queryOptions.DatabaseId, queryOptions.ContainerId, account.ConnectionString, queryOptions.Query, false)
	}
}

type DatabaseOptions struct {
	DatabaseId   string
	ContainerIds []string
}

type QueryOptions struct {
	DatabaseId  string
	ContainerId string
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
	queryOptions.DatabaseId = matches[2]
	queryOptions.ContainerId = matches[3]
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
	// TODO: Test this
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

func QueryAccountCmd() *cobra.Command {
	var (
		query          string
		accountName    string
		outputFormat   string
		outputFile     string
		verbose        bool
		debug          bool
		listAll        bool
		databaseId     string
		containerId    string
		savedQueryName string
	)

	queryAccountCmd := &cobra.Command{
		Use:   "query-account",
		Short: "Interact with your Cosmos DB account",
		Long: `Execute queries or list all documents in a Cosmos DB container.

Examples:
  alchemist query-account --query "SELECT * FROM database.container as c WHERE c.id = '123'"
  alchemist query-account --list-all --database mydb --container mycoll
  alchemist query-account --saved-query "my-query"`,
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			util.SetupLogging(verbose)

			dbManager := data.GetDefaultManager()

			if err := dbManager.OpenDatabase(); err != nil {
				handleDatabaseError(err)
				return
			}

			if err := dbManager.EnsureAccountTableExists(); err != nil {
				log.Error("Could not ensure account table exists", "error", err)
				return
			}

			// Check for saved query and load if needed.
			if savedQueryName != "" {
				loadedQuery, err := loadSavedQuery(savedQueryName)
				if err != nil {
					return
				}
				query = loadedQuery.QueryString
				if loadedQuery.AccountName != "" && accountName == "" {
					accountName = loadedQuery.AccountName
					log.Debug("Using account from saved query", "account", loadedQuery.AccountName)
				}
			}

			// Get account to use
			selectedAccount, err := getAccountToUse(accountName)
			if err != nil {
				return
			}
			log.Debug("Using account", "name", selectedAccount.Name)

			err = cosmos.Connect(selectedAccount.ConnectionString)
			if err != nil {
				log.Error("Failed to connect to Cosmos DB", "error", err)
				return
			}

			// Validate input parameters
			if !validateQueryParameters(query, listAll, databaseId, containerId) {
				return
			}

			// Handle list all documents request
			if listAll {
				if err := handleListAll(databaseId, containerId, selectedAccount.ConnectionString, outputFormat, outputFile, verbose); err != nil {
					return
				}
				return
			}

			// Process regular query
			if err := processQuery(query, selectedAccount.ConnectionString, outputFormat, outputFile, verbose); err != nil {
				return
			}
		},
	}

	// Add command flags
	addQueryFlags(queryAccountCmd, &query, &accountName, &outputFormat, &outputFile,
		&verbose, &debug, &listAll, &databaseId, &containerId, &savedQueryName)

	return queryAccountCmd
}

func handleDatabaseError(err error) {
	log.Error("Could not open database", "error", err)
	log.Info("Make sure you've added at least one account using 'alchemist add-account'")
}

func loadSavedQuery(queryName string) (*data.SavedQueryOptions, error) {
	dbManager := data.GetDefaultManager()

	if err := dbManager.EnsureSavedQueryTableExists(); err != nil {
		log.Error("Could not ensure saved query table exists", "error", err)
		return nil, err
	}

	savedQuery, err := dbManager.GetSavedQueryByName(queryName)
	if err != nil {
		log.Error("Could not find saved query", "name", queryName, "error", err)
		log.Info("Use 'alchemist list-query' to see all saved queries")
		return nil, err
	}

	log.Info("Using saved query", "name", savedQuery.Name)
	log.Debug("Query", "value", savedQuery.QueryString)

	return &savedQuery, nil
}

func getAccountToUse(accountName string) (data.AccountOptions, error) {
	dbManager := data.GetDefaultManager()

	accounts, err := dbManager.GetAccounts()
	if err != nil {
		log.Error("Could not retrieve accounts", "error", err)
		return data.AccountOptions{}, err
	}

	if len(accounts) == 0 {
		log.Info("No accounts found. Add an account using 'alchemist add-account'")
		return data.AccountOptions{}, fmt.Errorf("no accounts found")
	}

	if accountName != "" {
		for _, acc := range accounts {
			if acc.Name == accountName {
				return acc, nil
			}
		}
		log.Error("Account not found", "name", accountName)
		return data.AccountOptions{}, fmt.Errorf("account not found: %s", accountName)
	}

	// No account name specified, use default.
	for _, acc := range accounts {
		if acc.IsDefault {
			return acc, nil
		}
	}

	// No default found, use first account.
	log.Warn("No default account found. Using the first available account.")
	return accounts[0], nil
}

func validateQueryParameters(query string, listAll bool, databaseId, containerId string) bool {
	if query == "" && !listAll {
		log.Error("Missing required parameter. Either --query, --saved-query, or --list-all must be provided")
		log.Info("Use: alchemist query-account --query \"SELECT * FROM database.container as c\"")
		log.Info("  or: alchemist query-account --saved-query \"my-query\"")
		log.Info("  or: alchemist query-account --list-all --database <database_id> --container <container_id>")
		return false
	}

	if listAll && (databaseId == "" || containerId == "") {
		log.Error("Missing parameters. Database ID and Container ID are required with --list-all")
		log.Info("Use: alchemist query-account --list-all --database mydb --container mycoll")
		return false
	}

	return true
}

func handleListAll(databaseId, containerId, connectionString, outputFormat, outputFile string, verbose bool) error {
	log.Info("Listing all documents", "database", databaseId, "container", containerId)

	results, err := CrossPartitionQueryImpl(databaseId, containerId, connectionString, "SELECT * FROM c", verbose)
	if err != nil {
		log.Error("Error listing all documents", "error", err)
		return err
	}

	if results == "null" || results == "[]" {
		log.Info("No documents found in the container")
		return nil
	}

	return outputResults(results, outputFormat, outputFile)
}

func processQuery(query, connectionString, outputFormat, outputFile string, verbose bool) error {
	re := regexp.MustCompile(`FROM\s+([^.]+)\.(\S+)\s+as\s+c`)
	matches := re.FindStringSubmatch(query)
	if len(matches) < 3 {
		log.Error("Could not parse database and container from query")
		log.Info("Please use the format: SELECT * FROM database.container as c")
		return fmt.Errorf("invalid query format")
	}

	queryDatabaseId := matches[1]
	queryContainerId := matches[2]

	log.Info("Executing query", "database", queryDatabaseId, "container", queryContainerId)

	queryOptions, err := ExtractAndModifyQuery(query)
	if err != nil {
		log.Error("Could not parse query", "error", err)
		return err
	}

	log.Debug("Using SQL query", "query", queryOptions.Query)

	results, err := CrossPartitionQueryImpl(queryDatabaseId, queryContainerId, connectionString, queryOptions.Query, verbose)
	if err != nil {
		log.Error("Error executing query", "error", err)
		return err
	}

	if results == "null" || results == "[]" {
		log.Info("No documents found in the container")
		log.Info("This could be because:")
		log.Info("  - The container is empty")
		log.Info("  - Your query conditions don't match any documents")
		log.Info("  - There might be an issue with the query syntax")
		return nil
	}

	return outputResults(results, outputFormat, outputFile)
}

func outputResults(results, outputFormat, outputFile string) error {
	output, err := FormatOutput(results, OutputFormat(outputFormat))
	if err != nil {
		log.Error("Error formatting output", "error", err)
		return err
	}

	if outputFile != "" {
		err := os.WriteFile(outputFile, []byte(output), 0o600)
		if err != nil {
			log.Error("Error writing to file", "file", outputFile, "error", err)
			return err
		}
		log.Info("Results written to file", "file", outputFile)
	} else {
		fmt.Println(output)
	}

	return nil
}

func addQueryFlags(cmd *cobra.Command, query, accountName, outputFormat, outputFile *string,
	verbose, debug, listAll *bool, databaseId, containerId, savedQueryName *string) {
	cmd.Flags().StringVarP(query, "query", "q", "", "NoSQL query (required for non list-all operations)")

	cmd.Flags().StringVarP(accountName, "account", "a", "", "Account name to use (uses default if not specified)")

	cmd.Flags().StringVarP(outputFormat, "output-format", "o", string(JSON),
		"Output format: json, table, or raw")

	cmd.Flags().StringVarP(outputFile, "output-file", "f", "",
		"Write output to file instead of stdout")

	cmd.Flags().BoolVarP(verbose, "verbose", "v", false,
		"Show verbose output")

	cmd.Flags().BoolVarP(debug, "debug", "", false,
		"Show debug information")

	cmd.Flags().BoolVarP(listAll, "list-all", "l", false,
		"List all documents in a container (requires --database and --container)")

	cmd.Flags().StringVar(databaseId, "database", "", "Database ID (used with --list-all)")
	cmd.Flags().StringVar(containerId, "container", "", "Container ID (used with --list-all)")
	cmd.Flags().StringVar(savedQueryName, "saved-query", "", "Name of the saved query to run")
}
