package cosmos

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func CrossPartitionQuery(databaseId, containerId, connectionString string, customQuery string, verbose bool) (string, error) {
	var accountEndpoint, accountKey string
	parts := strings.Split(connectionString, ";")
	for _, part := range parts {
		if strings.HasPrefix(part, "AccountEndpoint=") {
			accountEndpoint = strings.TrimPrefix(part, "AccountEndpoint=")
		} else if strings.HasPrefix(part, "AccountKey=") {
			accountKey = strings.TrimPrefix(part, "AccountKey=")
		}
	}

	if accountEndpoint == "" || accountKey == "" {
		return "", fmt.Errorf("invalid connection string: missing AccountEndpoint or AccountKey")
	}

	parsedURL, err := url.Parse(accountEndpoint)
	if err != nil {
		return "", fmt.Errorf("failed to parse account endpoint: %v", err)
	}
	databaseAccount := strings.Split(parsedURL.Host, ".")[0]

	documentsUrl := fmt.Sprintf("https://%s.documents.azure.com/dbs/%s/colls/%s/docs",
		databaseAccount, url.PathEscape(databaseId), url.PathEscape(containerId))

	if verbose {
		fmt.Printf("Documents URL: %s\n", documentsUrl)
	}

	var req *http.Request
	var resourceType = "docs"
	var verb string

	if customQuery != "" {
		if verbose {
			fmt.Printf("Using custom query: %s\n", customQuery)
		}
		verb = "POST"

		requestBody := map[string]interface{}{
			"query":      customQuery,
			"parameters": []interface{}{},
		}
		requestBodyBytes, err := json.Marshal(requestBody)
		if err != nil {
			return "", fmt.Errorf("failed to marshal request body: %v", err)
		}

		req, err = http.NewRequest(verb, documentsUrl, bytes.NewBuffer(requestBodyBytes))
		if err != nil {
			return "", fmt.Errorf("failed to create HTTP request: %v", err)
		}

		req.Header.Set("Content-Type", "application/query+json")
		req.Header.Set("x-ms-documentdb-isquery", "true")
	} else {
		verb = "GET"
		req, err = http.NewRequest(verb, documentsUrl, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create HTTP request: %v", err)
		}
	}

	currentTime := time.Now().UTC().Format(http.TimeFormat)

	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-ms-date", currentTime)
	req.Header.Set("x-ms-version", "2018-12-31")
	req.Header.Set("x-ms-documentdb-query-enablecrosspartition", "true")
	req.Header.Set("x-ms-max-item-count", "100")

	resourceLink := fmt.Sprintf("dbs/%s/colls/%s", databaseId, containerId)
	token := generateAuthorizationToken(verb, resourceType, resourceLink, currentTime, accountKey)
	req.Header.Set("Authorization", token)

	if verbose {
		fmt.Printf("Executing direct cross-partition query using REST API %s /docs...\n", verb)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute HTTP request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("query failed with status code %d: %s", resp.StatusCode, string(body))
	}

	if verbose {
		fmt.Printf("Response status: %d\n", resp.StatusCode)
		fmt.Printf("Request charge: %s RUs\n", resp.Header.Get("x-ms-request-charge"))
	}

	var response struct {
		Documents []json.RawMessage `json:"Documents"`
		Count     int               `json:"_count"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return string(body), nil
	}

	if verbose {
		fmt.Printf("Found %d documents\n", len(response.Documents))
	}

	jsonData, err := json.MarshalIndent(response.Documents, "", "    ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal results to JSON: %v", err)
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
		fmt.Printf("Error decoding master key: %v\n", err)
		return ""
	}

	h := hmac.New(sha256.New, masterKey)
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	escapedSignature := url.QueryEscape(signature)

	authHeader := fmt.Sprintf("type=master&ver=1.0&sig=%s", escapedSignature)
	return authHeader
}
