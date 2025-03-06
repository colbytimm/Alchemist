package cmd

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
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

func ExtractAndModifyQuery(inputQuery string) (*QueryOptions, error) {
	if inputQuery == "" {
		return nil, errors.New("query cannot be empty, use --query flag to specify a query")
	}

	var queryOptions QueryOptions
	re := regexp.MustCompile(`SELECT\s+(.*\s+)?FROM\s+([^.]+)\.([^\s]+)\s+as\s+c\s*(.*)`)

	matches := re.FindStringSubmatch(inputQuery)
	if matches == nil || len(matches) < 5 {
		return nil, errors.New("invalid query format, expected: SELECT * FROM database.container as c WHERE ...")
	}

	queryOptions.DatabaseId = matches[2]
	queryOptions.ContainerId = matches[3]

	modifiedQuerySuffix := matches[4]
	if modifiedQuerySuffix != "" && modifiedQuerySuffix[:1] != " " {
		modifiedQuerySuffix = " " + modifiedQuerySuffix
	}

	queryOptions.Query = fmt.Sprintf("SELECT %sFROM docs c%s", matches[1], modifiedQuerySuffix)

	return &queryOptions, nil
}

func QueryAccountCmd() *cobra.Command {
	var query string

	queryAccountCmd := &cobra.Command{
		Use:                   "query-account",
		Short:                 "Query Cosmos DB accounts",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			// Open database with error handling
			err := data.OpenDatabase()
			if err != nil {
				fmt.Printf("Error: Could not open database: %v\n", err)
				fmt.Println("Hint: Make sure you've added at least one account using 'alchemist add-account'")
				return
			}

			// Check if account table exists and create it if it doesn't
			err = data.EnsureAccountTableExists()
			if err != nil {
				fmt.Printf("Error: Could not ensure account table exists: %v\n", err)
				return
			}

			// Get accounts with error handling
			accounts, err := data.GetAccounts()
			if err != nil {
				fmt.Printf("Error: Could not retrieve accounts: %v\n", err)
				return
			}
			
			// Check if there are any accounts
			if len(accounts) == 0 {
				fmt.Println("No accounts found. Add an account using 'alchemist add-account'")
				return
			}

			account := accounts[0]
			cosmos.Connect(account.ConnectionString)

			queryOptions, err := ExtractAndModifyQuery(query)
			if err != nil {
				fmt.Printf("Error: Could not parse query: %v\n", err)
				return
			}

			containerProperties, err := cosmos.GetContainerProperties(
				queryOptions.DatabaseId,
				queryOptions.ContainerId,
			)
			if err != nil {
				fmt.Printf("Error: Could not get container properties: %v\n", err)
				return
			}

			fmt.Printf("Query: %s, db: %s, container: %s\n", queryOptions.Query, queryOptions.DatabaseId, queryOptions.ContainerId)

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

			fmt.Printf("Request charge: %f\n", requestCharge)
			fmt.Println(items)
		},
	}
	queryAccountCmd.Flags().StringVarP(&query, "query", "q", "", "NoSQL query")

	return queryAccountCmd
}
