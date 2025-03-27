package cosmos

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

func ExtractCosmosCredentials(connectionString string) (accountEndpoint, accountKey string, err error) {
	parts := strings.Split(connectionString, ";")
	for _, part := range parts {
		if strings.HasPrefix(part, "AccountEndpoint=") {
			accountEndpoint = strings.TrimPrefix(part, "AccountEndpoint=")
		} else if strings.HasPrefix(part, "AccountKey=") {
			accountKey = strings.TrimPrefix(part, "AccountKey=")
		}
	}

	if accountEndpoint == "" || accountKey == "" {
		return "", "", fmt.Errorf("invalid connection string: missing AccountEndpoint or AccountKey")
	}

	return accountEndpoint, accountKey, nil
}

func CrossPartitionQuery(databaseId, containerId, connectionString, customQuery string, verbose bool) (string, error) {
	accountEndpoint, accountKey, err := ExtractCosmosCredentials(connectionString)
	if err != nil {
		return "", err
	}

	parsedURL, err := url.Parse(accountEndpoint)
	if err != nil {
		return "", fmt.Errorf("failed to parse account endpoint: %w", err)
	}
	databaseAccount := strings.Split(parsedURL.Host, ".")[0]

	documentsUrl := fmt.Sprintf("https://%s.documents.azure.com/dbs/%s/colls/%s/docs",
		databaseAccount, url.PathEscape(databaseId), url.PathEscape(containerId))

	if verbose {
		log.Debug("Documents URL", "url", documentsUrl)
	}

	var req *http.Request
	var resourceType = "docs"
	var verb string

	if customQuery != "" {
		if verbose {
			log.Debug("Using custom query", "query", customQuery)
		}
		verb = "POST"

		requestBody := map[string]interface{}{
			"query":      customQuery,
			"parameters": []interface{}{},
		}
		requestBodyJSON, err := json.Marshal(requestBody)
		if err != nil {
			return "", fmt.Errorf("failed to marshal request body: %w", err)
		}

		req, err = http.NewRequestWithContext(context.Background(), verb, documentsUrl, bytes.NewBuffer(requestBodyJSON))
		if err != nil {
			return "", fmt.Errorf("failed to create HTTP request: %w", err)
		}

		req.Header.Set("Content-Type", "application/query+json")
		req.Header.Set("x-ms-documentdb-isquery", "true")
	} else {
		verb = "GET"
		req, err = http.NewRequestWithContext(context.Background(), verb, documentsUrl, http.NoBody)
		if err != nil {
			return "", fmt.Errorf("failed to create HTTP request: %w", err)
		}
	}

	currentTime := time.Now().UTC().Format(http.TimeFormat)

	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-ms-date", currentTime)
	req.Header.Set("x-ms-version", "2018-12-31")
	req.Header.Set("x-ms-documentdb-query-enablecrosspartition", "true")
	// TODO: Add a setting to control this
	req.Header.Set("x-ms-max-item-count", "100")

	resourceLink := fmt.Sprintf("dbs/%s/colls/%s", databaseId, containerId)
	token := generateAuthorizationToken(verb, resourceType, resourceLink, currentTime, accountKey)
	req.Header.Set("Authorization", token)

	if verbose {
		log.Debug("Executing direct cross-partition query", "method", verb, "endpoint", "/docs")
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("query failed with status code %d: %s", resp.StatusCode, string(body))
	}

	if verbose {
		log.Debug("Response received", "status", resp.StatusCode, "requestCharge", resp.Header.Get("x-ms-request-charge")+" RUs")
	}

	var response struct {
		Documents []json.RawMessage `json:"Documents"`
		Count     int               `json:"_count"`
	}

	unmarshalErr := json.Unmarshal(body, &response)
	if unmarshalErr != nil {
		return string(body), nil
	}

	if verbose {
		log.Debug("Documents found", "count", len(response.Documents))
	}

	jsonData, err := json.MarshalIndent(response.Documents, "", "    ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal results to JSON: %w", err)
	}

	return string(jsonData), nil
}

func generateAuthorizationToken(verb, resourceType, resourceLink, date, key string) string {
	stringToSign := strings.ToLower(verb) + "\n" +
		strings.ToLower(resourceType) + "\n" +
		resourceLink + "\n" +
		strings.ToLower(date) + "\n" +
		"" + "\n"

	masterKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		log.Error("Error decoding master key", "error", err)
		return ""
	}

	h := hmac.New(sha256.New, masterKey)
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	escapedSignature := url.QueryEscape(signature)

	authHeader := fmt.Sprintf("type=master&ver=1.0&sig=%s", escapedSignature)
	return authHeader
}

type BatchUploadResult struct {
	Successful      int                      `json:"successful"`
	Failed          int                      `json:"failed"`
	TotalRUs        float64                  `json:"totalRUs"`
	FailedDocuments []map[string]interface{} `json:"failedDocuments,omitempty"`
	Errors          []string                 `json:"errors,omitempty"`
}

type BatchUploadOptions struct {
	BatchSize    int
	Retry        bool
	MaxRetries   int
	Verbose      bool
	BatchPauseMs int
}

func defaultBatchUploadOptions() BatchUploadOptions {
	return BatchUploadOptions{
		BatchSize:    100,
		Retry:        true,
		MaxRetries:   3,
		Verbose:      false,
		BatchPauseMs: 100,
	}
}

// BatchUpload uploads multiple documents to a Cosmos DB container in batches.
// Documents should be a slice of maps where each map contains the document properties.
// Each document must have a unique "id" field, and should have the partition key field.
func BatchUpload(databaseId, containerId, connectionString string, documents []map[string]interface{}, options *BatchUploadOptions) (*BatchUploadResult, error) {
	opts := getUploadOptions(options)

	documentsUrl, accountKey, err := prepareDocumentsUrl(connectionString, databaseId, containerId)
	if err != nil {
		return nil, err
	}

	result := &BatchUploadResult{
		Successful:      0,
		Failed:          0,
		TotalRUs:        0,
		FailedDocuments: []map[string]interface{}{},
		Errors:          []string{},
	}

	processDocumentsInBatches(documents, opts, documentsUrl, accountKey, result)

	return result, nil
}

func getUploadOptions(options *BatchUploadOptions) *BatchUploadOptions {
	opts := defaultBatchUploadOptions()
	if options != nil {
		if options.BatchSize > 0 {
			opts.BatchSize = options.BatchSize
		}
		opts.Retry = options.Retry
		if options.MaxRetries > 0 {
			opts.MaxRetries = options.MaxRetries
		}
		opts.Verbose = options.Verbose
		if options.BatchPauseMs > 0 {
			opts.BatchPauseMs = options.BatchPauseMs
		}
	}
	return &opts
}

func prepareDocumentsUrl(connectionString, databaseId, containerId string) (documentsUrl, accountKey string, err error) {
	var accountEndpoint string
	accountEndpoint, accountKey, err = ExtractCosmosCredentials(connectionString)
	if err != nil {
		return "", "", err
	}

	var parsedURL *url.URL
	parsedURL, err = url.Parse(accountEndpoint)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse account endpoint: %w", err)
	}
	databaseAccount := strings.Split(parsedURL.Host, ".")[0]

	documentsUrl = fmt.Sprintf("https://%s.documents.azure.com/dbs/%s/colls/%s/docs",
		databaseAccount, url.PathEscape(databaseId), url.PathEscape(containerId))

	return documentsUrl, accountKey, nil
}

func processDocumentsInBatches(documents []map[string]interface{}, opts *BatchUploadOptions, documentsUrl, accountKey string, result *BatchUploadResult) {
	// Process in batches
	totalBatches := (len(documents) + opts.BatchSize - 1) / opts.BatchSize
	for batchNum := 0; batchNum < totalBatches; batchNum++ {
		start := batchNum * opts.BatchSize
		end := start + opts.BatchSize
		if end > len(documents) {
			end = len(documents)
		}

		batchDocuments := documents[start:end]

		if opts.Verbose {
			log.Info("Processing batch", "batch", batchNum+1, "of", totalBatches, "documents", len(batchDocuments))
		}

		processDocumentBatch(batchDocuments, opts, documentsUrl, accountKey, result)

		// Pause between batches to avoid rate limiting
		if batchNum < totalBatches-1 && opts.BatchPauseMs > 0 {
			time.Sleep(time.Duration(opts.BatchPauseMs) * time.Millisecond)
		}
	}
}

func processDocumentBatch(documents []map[string]interface{}, opts *BatchUploadOptions, documentsUrl, accountKey string, result *BatchUploadResult) {
	for _, doc := range documents {
		if !isValidDocument(doc, result) {
			continue
		}

		docBytes, err := json.Marshal(doc)
		if err != nil {
			handleFailedDocument(doc, fmt.Sprintf("failed to marshal document: %v", err), result)
			continue
		}

		uploadWithRetries(doc, docBytes, opts, documentsUrl, accountKey, result)
	}
}

func isValidDocument(doc map[string]interface{}, result *BatchUploadResult) bool {
	if _, hasID := doc["id"]; !hasID {
		handleFailedDocument(doc, "document missing 'id' field", result)
		return false
	}
	return true
}

func handleFailedDocument(doc map[string]interface{}, errorMsg string, result *BatchUploadResult) {
	result.Failed++
	result.FailedDocuments = append(result.FailedDocuments, doc)
	result.Errors = append(result.Errors, errorMsg)
}

func uploadWithRetries(doc map[string]interface{}, docBytes []byte, opts *BatchUploadOptions, documentsUrl, accountKey string, result *BatchUploadResult) {
	success := false
	var lastError error

	for retryCount := 0; retryCount <= opts.MaxRetries; retryCount++ {
		if retryCount > 0 && opts.Verbose {
			log.Info("Retrying document upload", "id", doc["id"], "retry", retryCount)
		}

		uploaded, rus, err := uploadSingleDocument(documentsUrl, docBytes, accountKey, opts.Verbose)
		result.TotalRUs += rus

		if err == nil && uploaded {
			success = true
			result.Successful++
			break
		}

		lastError = err
		if !opts.Retry {
			break
		}
	}

	if !success {
		errorMsg := "unknown error during upload"
		if lastError != nil {
			errorMsg = lastError.Error()
		}
		handleFailedDocument(doc, errorMsg, result)
	}
}

func uploadSingleDocument(documentsUrl string, docBytes []byte, accountKey string, verbose bool) (success bool, requestCharge float64, err error) {
	req, err := http.NewRequestWithContext(context.Background(), "POST", documentsUrl, bytes.NewBuffer(docBytes))
	if err != nil {
		return false, 0, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	currentTime := time.Now().UTC().Format(http.TimeFormat)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-ms-date", currentTime)
	req.Header.Set("x-ms-version", "2018-12-31")

	// Extract database and collection IDs from URL
	urlParts := strings.Split(documentsUrl, "/")
	if len(urlParts) < 6 {
		return false, 0, fmt.Errorf("invalid documents URL format")
	}

	databaseId := ""
	containerId := ""
	for i, part := range urlParts {
		if part == "dbs" && i+1 < len(urlParts) {
			databaseId = urlParts[i+1]
		} else if part == "colls" && i+1 < len(urlParts) {
			containerId = urlParts[i+1]
		}
	}

	if databaseId == "" || containerId == "" {
		return false, 0, fmt.Errorf("failed to extract database or container ID from URL")
	}

	resourceLink := fmt.Sprintf("dbs/%s/colls/%s", databaseId, containerId)
	token := generateAuthorizationToken("post", "docs", resourceLink, currentTime, accountKey)
	req.Header.Set("Authorization", token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	requestCharge = 0.0
	if chargeStr := resp.Header.Get("x-ms-request-charge"); chargeStr != "" {
		fmt.Sscanf(chargeStr, "%f", &requestCharge)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if verbose {
			log.Info("Document created successfully", "requestCharge", requestCharge)
		}
		return true, requestCharge, nil
	}

	body, _ := io.ReadAll(resp.Body)
	errorMsg := fmt.Sprintf("upload failed with status code %d: %s", resp.StatusCode, string(body))

	if verbose {
		log.Error("Document upload failed", "status", resp.StatusCode, "requestCharge", requestCharge, "error", errorMsg)
	}

	return false, requestCharge, errors.New(errorMsg)
}
