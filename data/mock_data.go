package data

type MockDatabaseManager struct {
	OpenDatabaseMock                func() error
	EnsureAccountTableExistsMock    func() error
	InsertAccountMock               func(*AccountOptions) (AccountOptions, error)
	GetAccountByNameMock            func(string) (AccountOptions, error)
	GetAccountsMock                 func() ([]AccountOptions, error)
	DeleteAccountByNameMock         func(string) error
	UpdateDefaultItemMock           func(string) error
	EnsureSavedQueryTableExistsMock func() error
	SaveQueryMock                   func(*SavedQueryOptions) (SavedQueryOptions, error)
	GetSavedQueriesMock             func() ([]SavedQueryOptions, error)
	GetSavedQueryByNameMock         func(string) (SavedQueryOptions, error)
	DeleteSavedQueryByNameMock      func(string) error
	CloseMock                       func() error
}

func NewMockDatabaseManager() *MockDatabaseManager {
	return &MockDatabaseManager{
		OpenDatabaseMock: func() error {
			return nil
		},
		EnsureAccountTableExistsMock: func() error {
			return nil
		},
		InsertAccountMock: func(options *AccountOptions) (AccountOptions, error) {
			options.Id = 1
			return *options, nil
		},
		GetAccountByNameMock: func(name string) (AccountOptions, error) {
			return AccountOptions{
				Id:               1,
				Name:             name,
				ConnectionString: "AccountEndpoint=https://mock.documents.azure.com:443/;AccountKey=mock;",
				Tag:              "mock",
				IsDefault:        true,
			}, nil
		},
		GetAccountsMock: func() ([]AccountOptions, error) {
			return []AccountOptions{
				{
					Id:               1,
					Name:             "mock-account",
					ConnectionString: "AccountEndpoint=https://mock.documents.azure.com:443/;AccountKey=mock;",
					Tag:              "mock",
					IsDefault:        true,
				},
			}, nil
		},
		DeleteAccountByNameMock: func(name string) error {
			return nil
		},
		UpdateDefaultItemMock: func(name string) error {
			return nil
		},
		EnsureSavedQueryTableExistsMock: func() error {
			return nil
		},
		SaveQueryMock: func(options *SavedQueryOptions) (SavedQueryOptions, error) {
			options.Id = 1
			return *options, nil
		},
		GetSavedQueriesMock: func() ([]SavedQueryOptions, error) {
			return []SavedQueryOptions{
				{
					Id:           1,
					Name:         "mock-query",
					QueryString:  "SELECT * FROM c",
					DatabaseID:   "mock-db",
					ContainerID:  "mock-container",
					AccountName:  "mock-account",
					Description:  "A mock query",
					DateCreated:  "2023-01-01",
					DateModified: "2023-01-01",
				},
			}, nil
		},
		GetSavedQueryByNameMock: func(name string) (SavedQueryOptions, error) {
			return SavedQueryOptions{
				Id:           1,
				Name:         name,
				QueryString:  "mock-query",
				DatabaseID:   "mock-db",
				ContainerID:  "mock-container",
				AccountName:  "",
				Description:  "mock description",
				DateCreated:  "mock-date",
				DateModified: "mock-date",
			}, nil
		},
		DeleteSavedQueryByNameMock: func(name string) error {
			return nil
		},
		CloseMock: func() error {
			return nil
		},
	}
}

func (m *MockDatabaseManager) OpenDatabase() error {
	return m.OpenDatabaseMock()
}

func (m *MockDatabaseManager) EnsureAccountTableExists() error {
	return m.EnsureAccountTableExistsMock()
}

func (m *MockDatabaseManager) InsertAccount(options *AccountOptions) (AccountOptions, error) {
	return m.InsertAccountMock(options)
}

func (m *MockDatabaseManager) GetAccountByName(name string) (AccountOptions, error) {
	return m.GetAccountByNameMock(name)
}

func (m *MockDatabaseManager) GetAccounts() ([]AccountOptions, error) {
	return m.GetAccountsMock()
}

func (m *MockDatabaseManager) DeleteAccountByName(name string) error {
	return m.DeleteAccountByNameMock(name)
}

func (m *MockDatabaseManager) UpdateDefaultItem(name string) error {
	return m.UpdateDefaultItemMock(name)
}

func (m *MockDatabaseManager) EnsureSavedQueryTableExists() error {
	return m.EnsureSavedQueryTableExistsMock()
}

func (m *MockDatabaseManager) SaveQuery(options *SavedQueryOptions) (SavedQueryOptions, error) {
	return m.SaveQueryMock(options)
}

func (m *MockDatabaseManager) GetSavedQueries() ([]SavedQueryOptions, error) {
	return m.GetSavedQueriesMock()
}

func (m *MockDatabaseManager) GetSavedQueryByName(name string) (SavedQueryOptions, error) {
	return m.GetSavedQueryByNameMock(name)
}

func (m *MockDatabaseManager) DeleteSavedQueryByName(name string) error {
	return m.DeleteSavedQueryByNameMock(name)
}

func (m *MockDatabaseManager) Close() error {
	if m.CloseMock != nil {
		return m.CloseMock()
	}
	return nil
}
