package data

// AccountStore defines the interface for account-related operations.
type AccountStore interface {
	InsertAccount(options *AccountOptions) (AccountOptions, error)
	GetAccountByName(name string) (AccountOptions, error)
	GetAccounts() ([]AccountOptions, error)
	DeleteAccountByName(name string) error
	UpdateDefaultItem(name string) error
}

// QueryStore defines the interface for saved query operations.
type QueryStore interface {
	SaveQuery(options *SavedQueryOptions) (SavedQueryOptions, error)
	GetSavedQueryByName(name string) (SavedQueryOptions, error)
	GetSavedQueries() ([]SavedQueryOptions, error)
	DeleteSavedQueryByName(name string) error
}

// Store combines all storage operations.
type Store interface {
	AccountStore
	QueryStore
	Open() error
	Close() error
}
