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

## Examples

```bash
# Add a new Cosmos DB account
alchemist add-account --name "dev-account" --connection-string "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=your-key;" --default

# List all configured accounts
alchemist list-account

# Query data from a container
alchemist query-account --query "SELECT * FROM mydb.users as c WHERE c.country = 'USA'"
```

## Data Storage

Alchemist stores account information locally in an SQLite database file (`alchemist-database.db`) in the directory where the command is run.

## License

[License information]
