# Alchemist

A powerful command-line interface (CLI) tool for interacting with Azure Cosmos DB directly from your terminal.

## Overview

Alchemist simplifies working with Azure Cosmos DB by providing a convenient terminal interface for common operations. It eliminates the need to use the Azure portal or other GUI tools for basic database interactions.

## Installation

### Prerequisites
- Go 1.16 or later

### Building from source
```bash
# Clone the repository
git clone https://github.com/colbytimm/alchemist.git
cd alchemist

# Build the binary
go build -o alchemist

# Move to a directory in your PATH (optional)
mv alchemist /usr/local/bin/
```

## Usage

```bash
alchemist [command] [flags]
```

## Available Commands

### Account Management

#### Add Account
```bash
alchemist add-account --name <account_name> --connection-string <connection_string> [--tag <tag>] [--default] [--verbose]
```

#### Delete Account
```bash
alchemist delete-account --name <account_name> [--verbose]
```

#### List Accounts
```bash
alchemist list-account
```

The list-account command provides an interactive interface where you can:
- View all configured accounts
- Set an account as default by selecting it and pressing 'd' or Enter
- Delete an account by selecting it and pressing 'x' (with confirmation)
- Refresh the account list by pressing 'r'

#### Account Details
```bash
alchemist account-details [--account <account_name>] [--verbose]
```

The account-details command displays a table of all databases in a Cosmos DB account, showing:
- Database ID
- Number of containers in each database

This interactive table view allows you to:
- Navigate between databases using arrow keys
- View detailed container information by pressing Enter on a selected database
- See container properties including ID, partition key, and indexing mode
- Return to the database list with Backspace or Escape

This provides a clean and intuitive interface for exploring your Cosmos DB account structure.

### Querying

#### Query Account
```bash
alchemist query-account --query "SELECT * FROM database.container as c WHERE c.id = '123'" [--verbose]
```

The query syntax follows the Cosmos DB SQL syntax, with the database and container specified in the FROM clause.

All queries are executed as cross-partition queries by default using the Cosmos DB REST API, retrieving documents across all partitions regardless of their partition key.

#### List All Documents
```bash
alchemist query-account --list-all --database <database_id> --container <container_id> [--verbose]
```

This command lists all documents in a container without requiring a SQL query.

## Examples

```bash
# Add a new Cosmos DB account
alchemist add-account --name "dev-account" --connection-string "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=your-key;" --default

# List all configured accounts
alchemist list-account

# Query data from a container with a SQL query
alchemist query-account --query "SELECT * FROM mydb.users as c WHERE c.country = 'USA'"

# List all documents in a container
alchemist query-account --list-all --database mydb --container users

# Output query results in table format
alchemist query-account --query "SELECT * FROM mydb.orders as c" --output-format table

# Save query results to a file
alchemist query-account --query "SELECT * FROM mydb.logs as c" --output-file results.json

# Show verbose output with request details
alchemist query-account --query "SELECT * FROM mydb.users as c" --verbose

# Display database list from the default Cosmos DB account
alchemist account-details

# Display database list for a specific account
alchemist account-details --account "dev-account"

# Display database list with verbose debug logging
alchemist account-details --verbose

# Delete an account with detailed logging
alchemist delete-account --name "dev-account" --verbose
```

## Data Storage

Alchemist stores account information locally in an SQLite database file (`alchemist-database.db`) in the directory where the command is run.

## Development

### Running Tests

Alchemist includes a comprehensive test suite that uses in-memory databases to avoid creating real files during testing.

```bash
# Run all tests
go test ./... -v

# Run tests for a specific package
go test ./cmd/test/... -v

# Run specific tests matching a pattern
go test ./cmd/test/... -run TestDeleteAccount

# Run tests with short flag (skips long-running tests)
go test ./... -short
```


When writing new features, make sure to add appropriate unit tests and run the test suite to ensure everything works as expected.

## Features

Current features:
- [x] Account management (add, list, delete)
- [x] Database and container exploration
- [x] SQL query execution against Cosmos DB containers
- [x] Cross-partition querying via REST API
- [x] Batch document upload with retry mechanism
- [x] Local account credentials storage via SQLite
- [x] Interactive TUI for exploring databases and containers
- [] Simulate joins across containers
- [] Ability to save queries
- [] Settings to configure hard coded defaults

## License

[License information]
