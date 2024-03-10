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
	var queryOptions QueryOptions
	re := regexp.MustCompile(`SELECT\s+(.*\s+)?FROM\s+([^.]+)\.([^\s]+)\s+as\s+c\s*(.*)`)

	matches := re.FindStringSubmatch(inputQuery)
	if matches == nil || len(matches) < 5 {
		return nil, errors.New("cosmos client is nil")
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
			data.OpenDatabase()
			accounts := data.GetAccounts()
			account := accounts[0]
			cosmos.Connect(account.ConnectionString)
			queryOptions, _ := ExtractAndModifyQuery(query)
			containerProperties, _ := cosmos.GetContainerProperties(
				queryOptions.DatabaseId,
				queryOptions.ContainerId,
			)
			fmt.Printf("Query: %s, db: %s, container: %s", queryOptions.Query, queryOptions.DatabaseId, queryOptions.ContainerId)

			items, requestCharge := cosmos.ReadQuery(
				queryOptions.DatabaseId,
				queryOptions.ContainerId,
				queryOptions.Query,
				containerProperties.PartitionKeyDefinition.Paths[0],
			)
			fmt.Printf("Request charge: %f", requestCharge)
			fmt.Print(items)
		},
	}
	queryAccountCmd.Flags().StringVarP(&query, "query", "q", "", "NoSQL query")

	return queryAccountCmd
}
