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
alchemist add-account --name <account_name> --connection-string <connection_string> [--tag <tag>] [--default]
```

#### Delete Account
```bash
alchemist delete-account --name <account_name>
```

#### List Accounts
```bash
alchemist list-account
```

### Querying

#### Query Account
```bash
alchemist query-account --query "SELECT * FROM database.container as c WHERE c.id = '123'"
```

The query syntax follows the Cosmos DB SQL syntax, with the database and container specified in the FROM clause.

All queries are executed as cross-partition queries by default using the Cosmos DB REST API, retrieving documents across all partitions regardless of their partition key.

#### List All Documents
```bash
alchemist query-account --list-all --database <database_id> --container <container_id>
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
```

## Data Storage

Alchemist stores account information locally in an SQLite database file (`alchemist-database.db`) in the directory where the command is run.

## License

[License information]
