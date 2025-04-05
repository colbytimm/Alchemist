package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/colbytimm/alchemist/util"
	"github.com/spf13/cobra"
)

func BatchUploadInternal(
	accountName, databaseId, containerId, inputFile string,
	batchSize int, retry bool, maxRetries int, verbose bool, batchPauseMs int,
	dbManager data.DatabaseManager,
	cosmosManager cosmos.CosmosManager,
) (*cosmos.BatchUploadResult, error) {
	if databaseId == "" || containerId == "" || inputFile == "" {
		return nil, errors.New("database ID, container ID, and input file are required")
	}

	err := dbManager.OpenDatabase()
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}

	accounts, err := dbManager.GetAccounts()
	if err != nil {
		return nil, fmt.Errorf("could not retrieve accounts: %w", err)
	}

	if len(accounts) == 0 {
		return nil, errors.New("no accounts found")
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
			return nil, fmt.Errorf("account not found: %s", accountName)
		}
	}

	// Read input file
	fileData, err := os.ReadFile(inputFile)
	if err != nil {
		return nil, fmt.Errorf("error reading input file: %w", err)
	}

	// Parse JSON data
	var documents []map[string]interface{}
	err = json.Unmarshal(fileData, &documents)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	options := &cosmos.BatchUploadOptions{
		BatchSize:    batchSize,
		Retry:        retry,
		MaxRetries:   maxRetries,
		Verbose:      verbose,
		BatchPauseMs: batchPauseMs,
	}

	result, err := cosmosManager.BatchUpload(
		databaseId,
		containerId,
		account.ConnectionString,
		documents,
		options,
	)
	if err != nil {
		return nil, fmt.Errorf("batch upload failed: %w", err)
	}

	return result, nil
}

func BatchUploadCmd(sp *services.ServiceProvider) *cobra.Command {
	var (
		accountName  string
		databaseId   string
		containerId  string
		inputFile    string
		batchSize    int
		retry        bool
		maxRetries   int
		verbose      bool
		batchPauseMs int
	)

	batchUploadCmd := &cobra.Command{
		Use:                   "batch-upload",
		Short:                 "Upload multiple documents to Cosmos DB container",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			util.SetupLogging(verbose)

			result, err := BatchUploadInternal(
				accountName,
				databaseId,
				containerId,
				inputFile,
				batchSize,
				retry,
				maxRetries,
				verbose,
				batchPauseMs,
				sp.DatabaseManager,
				sp.CosmosManager,
			)
			if err != nil {
				log.Error(err.Error())
				return
			}

			log.Info(
				"Batch upload completed",
				"successful", result.Successful,
				"failed", result.Failed,
				"totalRUs", result.TotalRUs,
			)

			if result.Failed > 0 && len(result.Errors) > 0 {
				log.Error("Errors during upload:")
				for _, errMsg := range result.Errors {
					log.Error("- " + errMsg)
				}
			}
		},
	}

	batchUploadCmd.Flags().StringVarP(&accountName, "account", "a", "", "Account name to use (default if not specified)")
	batchUploadCmd.Flags().StringVarP(&databaseId, "database", "d", "", "Database ID")
	if err := batchUploadCmd.MarkFlagRequired("database"); err != nil {
		log.Fatal("Failed to mark 'database' flag as required", "error", err)
	}
	batchUploadCmd.Flags().StringVarP(&containerId, "container", "c", "", "Container ID")
	if err := batchUploadCmd.MarkFlagRequired("container"); err != nil {
		log.Fatal("Failed to mark 'container' flag as required", "error", err)
	}
	batchUploadCmd.Flags().StringVarP(&inputFile, "input", "i", "", "Input JSON file containing documents")
	if err := batchUploadCmd.MarkFlagRequired("input"); err != nil {
		log.Fatal("Failed to mark 'input' flag as required", "error", err)
	}
	batchUploadCmd.Flags().IntVar(&batchSize, "batch-size", 100, "Number of documents per batch")
	batchUploadCmd.Flags().BoolVar(&retry, "retry", true, "Retry failed documents")
	batchUploadCmd.Flags().IntVar(&maxRetries, "max-retries", 3, "Maximum number of retries")
	batchUploadCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output for debug logging")
	batchUploadCmd.Flags().IntVar(&batchPauseMs, "batch-pause", 100, "Pause between batches in milliseconds")

	return batchUploadCmd
}
