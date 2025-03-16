package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

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

	cmd := &cobra.Command{
		Use:                   "batch-upload",
		Short:                 "Upload multiple documents to a Cosmos DB container",
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			// Configure logger
			log.SetReportTimestamp(false)
			if verbose {
				log.SetLevel(log.DebugLevel)
			} else {
				log.SetLevel(log.InfoLevel)
			}

			// Validate parameters
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

			// Read documents from file
			log.Info("Reading documents from file", "file", jsonFile)
			fileData, err := os.ReadFile(jsonFile)
			if err != nil {
				log.Fatal("Failed to read JSON file", "error", err)
				return
			}

			var documents []map[string]interface{}
			if err := json.Unmarshal(fileData, &documents); err != nil {
				log.Fatal("Failed to parse JSON file", "error", err)
				return
			}

			log.Info("Documents to upload", "count", len(documents))

			// Get account connection string
			err = data.OpenDatabase()
			if err != nil {
				log.Fatal("Error opening database", "error", err)
				log.Info("Make sure you've added at least one account with 'alchemist add-account'")
				return
			}

			accounts, err := data.GetAccounts()
			if err != nil {
				log.Fatal("Error getting accounts", "error", err)
				return
			}

			if len(accounts) == 0 {
				log.Fatal("No accounts found. Add an account using 'alchemist add-account'")
				return
			}

			var account data.AccountOptions
			if accountName == "" {
				// Find default account
				defaultFound := false
				for _, acc := range accounts {
					if acc.IsDefault {
						account = acc
						defaultFound = true
						break
					}
				}

				if !defaultFound {
					account = accounts[0]
					log.Warn("No default account found. Using the first available account.")
				}
			} else {
				// Find specified account
				found := false
				for _, acc := range accounts {
					if acc.Name == accountName {
						account = acc
						found = true
						break
					}
				}

				if !found {
					log.Fatal("Account not found", "name", accountName)
					return
				}
			}

			log.Info("Using account", "name", account.Name)

			// Add partition key if specified
			if partitionKeyGen != "" {
				log.Info("Adding partition key field", "field", partitionKeyGen)

				for i := range documents {
					// Add partition key to documents that don't have it
					if _, hasPartKey := documents[i][partitionKeyGen]; !hasPartKey {
						// Use the document ID as partition key if present, otherwise use a random value
						if id, hasId := documents[i]["id"]; hasId {
							documents[i][partitionKeyGen] = id
						} else {
							documents[i][partitionKeyGen] = fmt.Sprintf("pk-%d", i)
						}
					}
				}
			}

			// Create upload options
			options := &cosmos.BatchUploadOptions{
				BatchSize:    batchSize,
				Retry:        !noRetry,
				MaxRetries:   maxRetries,
				Verbose:      verbose,
				BatchPauseMs: batchPauseMs,
			}

			// Start timer
			startTime := time.Now()

			// Upload documents
			log.Info("Uploading documents to database", "database", databaseId, "container", containerId)
			result, err := cosmos.BatchUpload(databaseId, containerId, account.ConnectionString, documents, options)
			if err != nil {
				log.Fatal("Failed to upload documents", "error", err)
				return
			}

			// Calculate metrics
			duration := time.Since(startTime)
			docsPerSecond := float64(result.Successful) / duration.Seconds()
			rusPerSecond := result.TotalRUs / duration.Seconds()

			// Display results
			log.Info("Upload completed", "duration", duration.String())
			log.Info("Upload results",
				"successful", result.Successful,
				"failed", result.Failed,
				"total_requests", result.Successful+result.Failed,
				"total_RUs", result.TotalRUs,
				"docs_per_second", fmt.Sprintf("%.2f", docsPerSecond),
				"RUs_per_second", fmt.Sprintf("%.2f", rusPerSecond))

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
		},
	}

	cmd.Flags().StringVarP(&accountName, "account", "a", "", "Account name to use (uses default if not specified)")
	cmd.Flags().StringVarP(&databaseId, "database", "d", "", "Database ID (required)")
	cmd.Flags().StringVarP(&containerId, "container", "c", "", "Container ID (required)")
	cmd.Flags().StringVarP(&jsonFile, "file", "f", "", "JSON file containing documents (required)")
	cmd.Flags().IntVarP(&batchSize, "batch-size", "b", 100, "Maximum documents per batch")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	cmd.Flags().BoolVar(&noRetry, "no-retry", false, "Disable retries for failed documents")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 3, "Maximum retry attempts")
	cmd.Flags().IntVar(&batchPauseMs, "batch-pause", 100, "Pause between batches in milliseconds")
	cmd.Flags().StringVarP(&partitionKeyGen, "partition-key", "p", "", "Field to use as partition key (will be added if missing)")

	cmd.MarkFlagRequired("database")
	cmd.MarkFlagRequired("container")
	cmd.MarkFlagRequired("file")

	return cmd
}
