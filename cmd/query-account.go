package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/logrusorgru/aurora/v4"
	"github.com/spf13/cobra"
)

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
	// JSON outputs results in pretty-printed JSON format
	JSON OutputFormat = "json"
	// TABLE outputs results in a tabular format (when possible)
	TABLE OutputFormat = "table"
	// RAW outputs results in raw JSON format (not pretty-printed)
	RAW OutputFormat = "raw"
)

func ExtractAndModifyQuery(inputQuery string) (*QueryOptions, error) {
	if inputQuery == "" {
		return nil, errors.New("query cannot be empty, use --query flag to specify a query")
	}

	var queryOptions QueryOptions
	// Update regex to allow hyphens in database and container names
	re := regexp.MustCompile(`SELECT\s+(.*\s+)?FROM\s+([^.]+)\.([^\s]+)\s+as\s+c\s*(.*)`)

	matches := re.FindStringSubmatch(inputQuery)
	if matches == nil || len(matches) < 5 {
		return nil, errors.New("invalid query format, expected: SELECT * FROM database.container as c WHERE ...")
	}

	queryOptions.DatabaseId = matches[2]
	queryOptions.ContainerId = matches[3]

	selectPart := matches[1]
	if selectPart == "" {
		selectPart = "* "
	}

	modifiedQuerySuffix := matches[4]
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
	switch format {
	case JSON:
		// The data is already in pretty-printed JSON format
		return jsonData, nil
	case RAW:
		// Convert to raw (compact) JSON
		var data interface{}
		if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
			return "", fmt.Errorf("error parsing JSON: %v", err)
		}
		rawBytes, err := json.Marshal(data)
		if err != nil {
			return "", fmt.Errorf("error formatting JSON: %v", err)
		}
		return string(rawBytes), nil
	case TABLE:
		// Attempt to format as a table (simplified implementation)
		// A more robust implementation would analyze the data structure
		// and create an appropriate table
		return fmt.Sprintf("Table formatting not fully implemented yet.\n%s", jsonData), nil
	default:
		return jsonData, nil
	}
}

func QueryAccountCmd() *cobra.Command {
	var (
		query        string
		accountName  string
		outputFormat string
		outputFile   string
		verbose      bool
		debug        bool
		createTest   bool
		listAll      bool
	)

	queryAccountCmd := &cobra.Command{
		Use:   "query-account",
		Short: "Query Cosmos DB accounts",
		Long: `Execute SQL queries against Cosmos DB containers.

Query Format:
  SELECT * FROM database.container as c WHERE <condition>

Examples:
  alchemist query-account --query "SELECT * FROM mydb.users as c WHERE c.country = 'USA'"
  alchemist query-account --query "SELECT c.id, c.name FROM mydb.products as c" --account "dev-account"
  alchemist query-account --query "SELECT * FROM mydb.orders as c" --output-format table
  alchemist query-account --query "SELECT * FROM mydb.logs as c" --output-file results.json`,
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			err := data.OpenDatabase()
			if err != nil {
				fmt.Printf("Error: Could not open database: %v\n", err)
				fmt.Println("Hint: Make sure you've added at least one account using 'alchemist add-account'")
				return
			}

			err = data.EnsureAccountTableExists()
			if err != nil {
				fmt.Printf("Error: Could not ensure account table exists: %v\n", err)
				return
			}

			accounts, err := data.GetAccounts()
			if err != nil {
				fmt.Printf("Error: Could not retrieve accounts: %v\n", err)
				return
			}

			if len(accounts) == 0 {
				fmt.Println("No accounts found. Add an account using 'alchemist add-account'")
				return
			}

			var selectedAccount data.AccountOptions
			if accountName != "" {
				found := false
				for _, acc := range accounts {
					if acc.Name == accountName {
						selectedAccount = acc
						found = true
						break
					}
				}
				if !found {
					fmt.Printf("Error: Account '%s' not found\n", accountName)
					return
				}
			} else {
				defaultFound := false
				for _, acc := range accounts {
					if acc.IsDefault {
						selectedAccount = acc
						defaultFound = true
						break
					}
				}

				if !defaultFound {
					selectedAccount = accounts[0]
					fmt.Println("Warning: No default account found. Using the first available account.")
				}
			}

			if verbose {
				fmt.Printf("Using account: %s\n", selectedAccount.Name)
			}

			cosmos.Connect(selectedAccount.ConnectionString)

			re := regexp.MustCompile(`FROM\s+([^.]+)\.([^\s]+)\s+as\s+c`)
			matches := re.FindStringSubmatch(query)
			if matches == nil || len(matches) < 3 {
				fmt.Println("Error: Could not parse database and container from query")
				fmt.Println("Please use the format: SELECT * FROM database.container as c")
				return
			}

			databaseId := matches[1]
			containerId := matches[2]

			if createTest {
				docId, err := cosmos.CreateTestDocument(databaseId, containerId)
				if err != nil {
					fmt.Printf("Error creating test document: %v\n", err)
					return
				}
				fmt.Printf("Created test document with ID: %s\n", docId)
				fmt.Println("You can now query it with:")
				fmt.Printf("  alchemist query-account -q \"SELECT * FROM %s.%s as c WHERE c.id = '%s'\"\n",
					databaseId, containerId, docId)
				return
			}

			if listAll {
				fmt.Printf("Listing all documents in %s.%s...\n", databaseId, containerId)

				containerProperties, err := cosmos.GetContainerProperties(databaseId, containerId)
				if err != nil {
					fmt.Printf("Error: Could not get container properties: %v\n", err)
					return
				}

				partitionKeys, err := cosmos.GetPartitionKeyValues(databaseId, containerId)
				if err != nil {
					fmt.Printf("Error getting partition keys: %v\n", err)
					return
				}

				if len(partitionKeys) == 0 {
					fmt.Println("No partition key values found. The container appears to be empty.")
					return
				}

				fmt.Printf("Found %d partition key values\n", len(partitionKeys))

				var allItems []map[string]interface{}
				var totalRequestCharge float32

				for _, pkValue := range partitionKeys {
					fmt.Printf("Querying partition: %s\n", pkValue)

					partitionQuery := fmt.Sprintf("SELECT * FROM c WHERE c.%s = '%s'",
						strings.TrimPrefix(containerProperties.PartitionKeyDefinition.Paths[0], "/"),
						pkValue)

					items, requestCharge, err := cosmos.ReadQuery(
						databaseId,
						containerId,
						partitionQuery,
						containerProperties.PartitionKeyDefinition.Paths[0],
					)
					if err != nil {
						fmt.Printf("Error querying partition %s: %v\n", pkValue, err)
						continue
					}

					totalRequestCharge += requestCharge

					var partitionItems []map[string]interface{}
					err = json.Unmarshal([]byte(items), &partitionItems)
					if err != nil {
						fmt.Printf("Error parsing items from partition %s: %v\n", pkValue, err)
						continue
					}

					allItems = append(allItems, partitionItems...)
				}

				jsonData, err := json.MarshalIndent(allItems, "", "    ")
				if err != nil {
					fmt.Printf("Error formatting output: %v\n", err)
					return
				}

				output, err := FormatOutput(string(jsonData), OutputFormat(outputFormat))
				if err != nil {
					fmt.Printf("Error formatting output: %v\n", err)
					return
				}

				fmt.Printf("Total request charge: %f RUs\n", totalRequestCharge)
				fmt.Printf("Found %d documents\n", len(allItems))

				if len(allItems) == 0 {
					fmt.Println("No documents found in the container.")
				} else {
					fmt.Println(output)
				}

				return
			}

			containerProperties, err := cosmos.GetContainerProperties(databaseId, containerId)
			if err != nil {
				fmt.Printf("Error: Could not get container properties: %v\n", err)
				return
			}

			// Execute the query
			queryOptions, err := ExtractAndModifyQuery(query)
			if err != nil {
				fmt.Printf("Error: Could not parse query: %v\n", err)
				return
			}

			if verbose {
				fmt.Printf("Database: %s\nContainer: %s\nModified Query: %s\n",
					queryOptions.DatabaseId,
					queryOptions.ContainerId,
					queryOptions.Query)
				fmt.Printf("Partition Key Path: %s\n", containerProperties.PartitionKeyDefinition.Paths[0])
			}

			items, requestCharge, err := cosmos.ReadQuery(
				queryOptions.DatabaseId,
				queryOptions.ContainerId,
				queryOptions.Query,
				containerProperties.PartitionKeyDefinition.Paths[0],
			)
			if err != nil {
				fmt.Printf("Error: Could not execute query: %v\n", err)
				return
			}

			output, err := FormatOutput(items, OutputFormat(outputFormat))
			if err != nil {
				fmt.Printf("Error formatting output: %v\n", err)
				return
			}

			fmt.Printf("Request charge: %f RUs\n", requestCharge)

			if items == "[]" {
				fmt.Println(aurora.Yellow("Warning: Query returned no results. This could be because:"))
				fmt.Println("  - The container is empty")
				fmt.Println("  - Your query conditions don't match any documents")
				fmt.Println("  - There might be an issue with the query syntax")
				fmt.Println("\nTry creating a test document with --create-test")
			}

			if outputFile != "" {
				err := os.WriteFile(outputFile, []byte(output), 0644)
				if err != nil {
					fmt.Printf("Error writing to file %s: %v\n", outputFile, err)
					return
				}
				fmt.Printf("Results written to %s\n", outputFile)
			} else {
				fmt.Println(output)
			}
		},
	}

	queryAccountCmd.Flags().StringVarP(&query, "query", "q", "", "NoSQL query (required)")
	queryAccountCmd.MarkFlagRequired("query")

	queryAccountCmd.Flags().StringVarP(&accountName, "account", "a", "", "Account name to use (uses default if not specified)")

	queryAccountCmd.Flags().StringVarP(&outputFormat, "output-format", "o", string(JSON),
		"Output format: json, table, or raw")

	queryAccountCmd.Flags().StringVarP(&outputFile, "output-file", "f", "",
		"Write output to file instead of stdout")

	queryAccountCmd.Flags().BoolVarP(&verbose, "verbose", "v", false,
		"Show verbose output")

	queryAccountCmd.Flags().BoolVarP(&debug, "debug", "d", false,
		"Show debug information")

	queryAccountCmd.Flags().BoolVarP(&createTest, "create-test", "t", false,
		"Create a test document in the container")

	queryAccountCmd.Flags().BoolVarP(&listAll, "list-all", "l", false,
		"List all documents in the container")

	return queryAccountCmd
}
