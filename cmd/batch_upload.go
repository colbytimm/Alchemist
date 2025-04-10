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
	"github.com/colbytimm/alchemist/services"
	"github.com/colbytimm/alchemist/util"
	"github.com/spf13/cobra"
)

// secureReadFile safely reads a file with validation checks.
func secureReadFile(filePath string) ([]byte, error) {
	// Clean and normalize the path
	cleanPath := filepath.Clean(filePath)

	// Get absolute path
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("error getting absolute path: %w", err)
	}

	// Check file info
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("error accessing file: %w", err)
	}

	// Ensure it's a regular file
	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", absPath)
	}

	// Check file size
	if fileInfo.Size() > 100*1024*1024 { // 100MB limit
		return nil, fmt.Errorf("file too large: %d bytes", fileInfo.Size())
	}

	// Read file content
	// #nosec G304 - Path is cleaned, validated for existence, type, and size above
	fileData, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return fileData, nil
}

func BatchUploadInternal(
	accountName, databaseID, containerID, inputFile string,
	batchSize int, retry bool, maxRetries int, verbose bool, batchPauseMs int,
	dbManager data.DatabaseManager,
	cosmosManager cosmos.CosmosManager,
) (*cosmos.BatchUploadResult, error) {
	if databaseID == "" || containerID == "" || inputFile == "" {
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

	// Read input file using secure function
	fileData, err := secureReadFile(inputFile)
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
		databaseID,
		containerID,
		account.ConnectionString,
		documents,
		options,
	)
	if err != nil {
		return nil, fmt.Errorf("batch upload failed: %w", err)
	}

	return result, nil
}

func CosmosDataUploadCmd(sp *services.ServiceProvider) *cobra.Command {
	var (
		accountName  string
		databaseID   string
		containerID  string
		inputFile    string
		batchSize    int
		retry        bool
		maxRetries   int
		verbose      bool
		batchPauseMs int
	)

	cmd := &cobra.Command{
		Use:                   "data upload",
		Short:                 "Upload documents to a Cosmos DB container",
		Long:                  "Upload documents to a Cosmos DB container from a JSON file",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			util.SetupLogging(verbose)

			result, err := BatchUploadInternal(
				accountName,
				databaseID,
				containerID,
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

	cmd.Flags().StringVarP(&accountName, "account", "a", "", "Account name to use (default if not specified)")
	cmd.Flags().StringVarP(&databaseID, "database", "d", "", "Database ID")
	if err := cmd.MarkFlagRequired("database"); err != nil {
		log.Fatal("Failed to mark 'database' flag as required", "error", err)
	}
	cmd.Flags().StringVarP(&containerID, "container", "c", "", "Container ID")
	if err := cmd.MarkFlagRequired("container"); err != nil {
		log.Fatal("Failed to mark 'container' flag as required", "error", err)
	}
	cmd.Flags().StringVarP(&inputFile, "input", "i", "", "Input JSON file containing documents")
	if err := cmd.MarkFlagRequired("input"); err != nil {
		log.Fatal("Failed to mark 'input' flag as required", "error", err)
	}
	cmd.Flags().IntVar(&batchSize, "batch-size", 100, "Number of documents per batch")
	cmd.Flags().BoolVar(&retry, "retry", true, "Retry failed documents")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 3, "Maximum number of retries")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output for debug logging")
	cmd.Flags().IntVar(&batchPauseMs, "batch-pause", 100, "Pause between batches in milliseconds")

	return cmd
}
