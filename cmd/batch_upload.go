package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

var BatchUploadImpl = cosmos.BatchUpload

var ConnectImpl = cosmos.Connect

func BatchUploadInternal(accountName, databaseId, containerId, jsonFile string, options *cosmos.BatchUploadOptions) (*cosmos.BatchUploadResult, error) {
	if databaseId == "" {
		return nil, errors.New("database ID is required")
	}

	if containerId == "" {
		return nil, errors.New("container ID is required")
	}

	if jsonFile == "" {
		return nil, errors.New("JSON file is required")
	}

	fileExt := filepath.Ext(jsonFile)
	if fileExt != ".json" {
		return nil, fmt.Errorf("file must have .json extension: %s", jsonFile)
	}

	// Make sure the file exists.
	if _, err := os.Stat(jsonFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("file does not exist: %s", jsonFile)
	}

	dbManager := data.GetDefaultManager()

	err := dbManager.OpenDatabase()
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}

	var account data.AccountOptions
	var accounts []data.AccountOptions

	// Get accounts and resolve which one to use.
	accounts, err = dbManager.GetAccounts()
	if err != nil {
		return nil, fmt.Errorf("could not retrieve accounts: %w", err)
	}

	if len(accounts) == 0 {
		return nil, errors.New("no accounts found")
	}

	if accountName == "" {
		// Find default account.
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
		account, err = dbManager.GetAccountByName(accountName)
		if err != nil {
			return nil, fmt.Errorf("could not retrieve account: %w", err)
		}
	}

	// Read the JSON file.
	jsonData, err := os.ReadFile(jsonFile)
	if err != nil {
		return nil, fmt.Errorf("error reading JSON file: %w", err)
	}

	// Parse the JSON data.
	var documents []map[string]interface{}
	err = json.Unmarshal(jsonData, &documents)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON file: %w", err)
	}

	// Connect to Cosmos DB.
	err = ConnectImpl(account.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Cosmos DB: %w", err)
	}

	// Perform the batch upload.
	result, err := BatchUploadImpl(databaseId, containerId, account.ConnectionString, documents, options)
	if err != nil {
		return nil, fmt.Errorf("error during batch upload: %w", err)
	}

	return result, nil
}

func BatchUploadCmd() *cobra.Command {
	var (
		accountName     string
		databaseId      string
		containerId     string
		jsonFile        string
		batchSize       int
		verbose         bool
		noRetry         bool
		maxRetries      int
		batchPauseMs    int
		partitionKeyGen string
	)

	batchUploadCmd := &cobra.Command{
		Use:   "batch-upload",
		Short: "Upload documents in bulk to a Cosmos DB container",
		Long: `Upload multiple documents from a JSON file to a Cosmos DB container.

Examples:
  alchemist batch-upload --database mydb --container mycoll --file documents.json
  alchemist batch-upload --database mydb --container mycoll --file documents.json --account dev-account --verbose`,
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

			validateRequiredParams(databaseId, containerId, jsonFile)

			options := createBatchUploadOptions(batchSize, noRetry, maxRetries, verbose, batchPauseMs)

			result, err := BatchUploadInternal(accountName, databaseId, containerId, jsonFile, options)
			if err != nil {
				log.Error("Failed to upload documents", "error", err)
				return
			}

			displayResults(result)
		},
	}

	addBatchUploadFlags(batchUploadCmd, &accountName, &databaseId, &containerId, &jsonFile,
		&batchSize, &verbose, &noRetry, &maxRetries, &batchPauseMs, &partitionKeyGen)

	return batchUploadCmd
}

func validateRequiredParams(databaseId, containerId, jsonFile string) {
	if databaseId == "" {
		log.Fatal("Database ID is required")
		return
	}

	if containerId == "" {
		log.Fatal("Container ID is required")
		return
	}

	if jsonFile == "" {
		log.Fatal("JSON file is required")
		return
	}
}

func createBatchUploadOptions(batchSize int, noRetry bool, maxRetries int, verbose bool, batchPauseMs int) *cosmos.BatchUploadOptions {
	return &cosmos.BatchUploadOptions{
		BatchSize:    batchSize,
		Retry:        !noRetry,
		MaxRetries:   maxRetries,
		Verbose:      verbose,
		BatchPauseMs: batchPauseMs,
	}
}

func displayResults(result *cosmos.BatchUploadResult) {
	log.Info("Upload results",
		"successful", result.Successful,
		"failed", result.Failed,
		"total_requests", result.Successful+result.Failed,
		"total_RUs", result.TotalRUs)

	if result.Failed > 0 {
		log.Error("Some documents failed to upload", "count", result.Failed)

		if len(result.Errors) > 0 {
			log.Error("Error summary (first 5 errors):")
			for i, err := range result.Errors {
				if i >= 5 {
					break
				}
				log.Error(fmt.Sprintf("[%d] %s", i+1, err))
			}
		}
	}
}

func addBatchUploadFlags(cmd *cobra.Command, accountName, databaseId, containerId, jsonFile *string,
	batchSize *int, verbose, noRetry *bool, maxRetries, batchPauseMs *int, partitionKeyGen *string) {
	cmd.Flags().StringVarP(accountName, "account", "a", "", "Account name to use (uses default if not specified)")
	cmd.Flags().StringVarP(databaseId, "database", "d", "", "Database ID (required)")
	cmd.Flags().StringVarP(containerId, "container", "c", "", "Container ID (required)")
	cmd.Flags().StringVarP(jsonFile, "file", "f", "", "JSON file containing documents (required)")
	cmd.Flags().IntVarP(batchSize, "batch-size", "b", 100, "Maximum documents per batch")
	cmd.Flags().BoolVarP(verbose, "verbose", "v", false, "Enable verbose output")
	cmd.Flags().BoolVar(noRetry, "no-retry", false, "Disable retries for failed documents")
	cmd.Flags().IntVar(maxRetries, "max-retries", 3, "Maximum retry attempts")
	cmd.Flags().IntVar(batchPauseMs, "batch-pause", 100, "Pause between batches in milliseconds")
	cmd.Flags().StringVarP(partitionKeyGen, "partition-key", "p", "", "Field to use as partition key (will be added if missing)")

	if err := cmd.MarkFlagRequired("database"); err != nil {
		log.Fatal("Error marking database flag as required", "error", err)
	}
	if err := cmd.MarkFlagRequired("container"); err != nil {
		log.Fatal("Error marking container flag as required", "error", err)
	}
	if err := cmd.MarkFlagRequired("file"); err != nil {
		log.Fatal("Error marking file flag as required", "error", err)
	}
}
