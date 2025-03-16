package data

type DataManager interface {
	OpenDatabase() error
	EnsureAccountTableExists() error
	InsertAccount(*AccountOptions) (AccountOptions, error)
	GetAccountByName(string) (AccountOptions, error)
	GetAccounts() ([]AccountOptions, error)
	DeleteAccountByName(string) error
}

type DefaultDataManager struct{}

func (dm *DefaultDataManager) OpenDatabase() error {
	return OpenDatabase()
}

func (dm *DefaultDataManager) EnsureAccountTableExists() error {
	return EnsureAccountTableExists()
}

func (dm *DefaultDataManager) InsertAccount(options *AccountOptions) (AccountOptions, error) {
	return InsertAccount(options)
}

func (dm *DefaultDataManager) GetAccountByName(name string) (AccountOptions, error) {
	return GetAccountByName(name)
}

func (dm *DefaultDataManager) GetAccounts() ([]AccountOptions, error) {
	return GetAccounts()
}

func (dm *DefaultDataManager) DeleteAccountByName(name string) error {
	return DeleteAccountByName(name)
}
