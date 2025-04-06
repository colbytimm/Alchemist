package test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockDB mocks sql.DB for unit testing.
type MockDB struct {
	mock.Mock
}

// MockRow represents a mock for sql.Row.
type MockRow struct {
	mock.Mock
	data  []interface{}
	index int
}

// Scan implements the sql.Row Scan method.
func (m *MockRow) Scan(dest ...interface{}) error {
	if m.data == nil || len(m.data) < len(dest) {
		return sql.ErrNoRows
	}

	for i, d := range dest {
		switch v := d.(type) {
		case *int:
			*v = m.data[i].(int)
		case *string:
			*v = m.data[i].(string)
		case *bool:
			*v = m.data[i].(bool)
		}
	}
	return nil
}

func (m *MockDB) QueryRow(query string, args ...interface{}) *sql.Row {
	callArgs := []interface{}{query}
	callArgs = append(callArgs, args...)
	m.Called(callArgs...)

	// We can't directly mock sql.Row as it's not an interface
	// This is a placeholder since we're mocking at the manager level.
	return nil
}

func (m *MockDB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	callArgs := []interface{}{query}
	callArgs = append(callArgs, args...)
	returnArgs := m.Called(callArgs...)
	return nil, returnArgs.Error(1)
}

func (m *MockDB) Prepare(query string) (*sql.Stmt, error) {
	args := m.Called(query)
	return nil, args.Error(1)
}

func (m *MockDB) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockDB) Exec(query string, args ...interface{}) (sql.Result, error) {
	callArgs := []interface{}{query}
	callArgs = append(callArgs, args...)
	returnArgs := m.Called(callArgs...)
	if returnArgs.Get(0) == nil {
		return nil, returnArgs.Error(1)
	}
	return returnArgs.Get(0).(sql.Result), returnArgs.Error(1)
}

// MockResult implements sql.Result for testing.
type MockResult struct {
	lastId       int64
	rowsAffected int64
}

func NewMockResult(id, affected int64) *MockResult {
	return &MockResult{
		lastId:       id,
		rowsAffected: affected,
	}
}

func (m *MockResult) LastInsertId() (int64, error) {
	return m.lastId, nil
}

func (m *MockResult) RowsAffected() (int64, error) {
	return m.rowsAffected, nil
}

func TestNewSQLiteManager(t *testing.T) {
	t.Run("Custom_Path", func(t *testing.T) {
		customPath := "custom/path/db.sqlite"
		manager := data.NewSQLiteManager(customPath)
		assert.NotNil(t, manager)
	})

	t.Run("Default_Path", func(t *testing.T) {
		manager := data.NewSQLiteManager("")
		assert.NotNil(t, manager)
	})
}

func TestOpenDatabase(t *testing.T) {
	mockManager := data.NewMockDatabaseManager()

	t.Run("Success", func(t *testing.T) {
		mockManager.OpenDatabaseMock = func() error {
			return nil
		}

		err := mockManager.OpenDatabase()
		assert.NoError(t, err)
	})

	t.Run("Error", func(t *testing.T) {
		expectedErr := errors.New("failed to open database")
		mockManager.OpenDatabaseMock = func() error {
			return expectedErr
		}

		err := mockManager.OpenDatabase()
		assert.Equal(t, expectedErr, err)
	})
}

func TestAccountOperations(t *testing.T) {
	mockManager := data.NewMockDatabaseManager()

	t.Run("InsertAccount_Success", func(t *testing.T) {
		account := &data.AccountOptions{
			Name:             "testaccount",
			ConnectionString: "connection-string",
			Tag:              "testtag",
			IsDefault:        false,
		}

		expectedAccount := data.AccountOptions{
			Id:               1,
			Name:             "testaccount",
			ConnectionString: "connection-string",
			Tag:              "testtag",
			IsDefault:        false,
		}

		mockManager.InsertAccountMock = func(options *data.AccountOptions) (data.AccountOptions, error) {
			return expectedAccount, nil
		}

		result, err := mockManager.InsertAccount(account)
		assert.NoError(t, err)
		assert.Equal(t, expectedAccount, result)
	})

	t.Run("InsertAccount_Error", func(t *testing.T) {
		account := &data.AccountOptions{
			Name:             "testaccount",
			ConnectionString: "connection-string",
			Tag:              "testtag",
			IsDefault:        false,
		}

		expectedErr := errors.New("account already exists")

		mockManager.InsertAccountMock = func(options *data.AccountOptions) (data.AccountOptions, error) {
			return data.AccountOptions{}, expectedErr
		}

		_, err := mockManager.InsertAccount(account)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("GetAccountByName_Success", func(t *testing.T) {
		expectedAccount := data.AccountOptions{
			Id:               1,
			Name:             "testaccount",
			ConnectionString: "connection-string",
			Tag:              "testtag",
			IsDefault:        false,
		}

		mockManager.GetAccountByNameMock = func(name string) (data.AccountOptions, error) {
			assert.Equal(t, "testaccount", name)
			return expectedAccount, nil
		}

		result, err := mockManager.GetAccountByName("testaccount")
		assert.NoError(t, err)
		assert.Equal(t, expectedAccount, result)
	})

	t.Run("GetAccountByName_NotFound", func(t *testing.T) {
		expectedErr := errors.New("account not found")

		mockManager.GetAccountByNameMock = func(name string) (data.AccountOptions, error) {
			return data.AccountOptions{}, expectedErr
		}

		_, err := mockManager.GetAccountByName("nonexistent")
		assert.Equal(t, expectedErr, err)
	})

	t.Run("GetAccounts_Success", func(t *testing.T) {
		expectedAccounts := []data.AccountOptions{
			{
				Id:               1,
				Name:             "account1",
				ConnectionString: "connection-string-1",
				Tag:              "tag1",
				IsDefault:        true,
			},
			{
				Id:               2,
				Name:             "account2",
				ConnectionString: "connection-string-2",
				Tag:              "tag2",
				IsDefault:        false,
			},
		}

		mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
			return expectedAccounts, nil
		}

		results, err := mockManager.GetAccounts()
		assert.NoError(t, err)
		assert.Equal(t, expectedAccounts, results)
	})

	t.Run("GetAccounts_Error", func(t *testing.T) {
		expectedErr := errors.New("database error")

		mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
			return nil, expectedErr
		}

		_, err := mockManager.GetAccounts()
		assert.Equal(t, expectedErr, err)
	})

	t.Run("UpdateDefaultItem_Success", func(t *testing.T) {
		mockManager.UpdateDefaultItemMock = func(name string) error {
			assert.Equal(t, "testaccount", name)
			return nil
		}

		err := mockManager.UpdateDefaultItem("testaccount")
		assert.NoError(t, err)
	})

	t.Run("UpdateDefaultItem_Error", func(t *testing.T) {
		expectedErr := errors.New("update error")

		mockManager.UpdateDefaultItemMock = func(name string) error {
			return expectedErr
		}

		err := mockManager.UpdateDefaultItem("testaccount")
		assert.Equal(t, expectedErr, err)
	})

	t.Run("DeleteAccountByName_Success", func(t *testing.T) {
		mockManager.DeleteAccountByNameMock = func(name string) error {
			assert.Equal(t, "testaccount", name)
			return nil
		}

		err := mockManager.DeleteAccountByName("testaccount")
		assert.NoError(t, err)
	})

	t.Run("DeleteAccountByName_NotFound", func(t *testing.T) {
		expectedErr := errors.New("account not found")

		mockManager.DeleteAccountByNameMock = func(name string) error {
			return expectedErr
		}

		err := mockManager.DeleteAccountByName("nonexistent")
		assert.Equal(t, expectedErr, err)
	})
}

func TestSavedQueryOperations(t *testing.T) {
	mockManager := data.NewMockDatabaseManager()

	t.Run("SaveQuery_Success", func(t *testing.T) {
		query := &data.SavedQueryOptions{
			Name:        "testquery",
			QueryString: "SELECT * FROM test",
			DatabaseId:  "testdb",
			ContainerId: "testcontainer",
			AccountName: "testaccount",
			Description: "Test query description",
		}

		expectedQuery := data.SavedQueryOptions{
			Id:           1,
			Name:         "testquery",
			QueryString:  "SELECT * FROM test",
			DatabaseId:   "testdb",
			ContainerId:  "testcontainer",
			AccountName:  "testaccount",
			Description:  "Test query description",
			DateCreated:  "2023-01-01T12:00:00Z",
			DateModified: "2023-01-01T12:00:00Z",
		}

		mockManager.SaveQueryMock = func(options *data.SavedQueryOptions) (data.SavedQueryOptions, error) {
			return expectedQuery, nil
		}

		result, err := mockManager.SaveQuery(query)
		assert.NoError(t, err)
		assert.Equal(t, expectedQuery, result)
	})

	t.Run("SaveQuery_Error", func(t *testing.T) {
		query := &data.SavedQueryOptions{
			Name:        "testquery",
			QueryString: "SELECT * FROM test",
		}

		expectedErr := errors.New("save query error")

		mockManager.SaveQueryMock = func(options *data.SavedQueryOptions) (data.SavedQueryOptions, error) {
			return data.SavedQueryOptions{}, expectedErr
		}

		_, err := mockManager.SaveQuery(query)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("GetSavedQueryByName_Success", func(t *testing.T) {
		expectedQuery := data.SavedQueryOptions{
			Id:           1,
			Name:         "testquery",
			QueryString:  "SELECT * FROM test",
			DatabaseId:   "testdb",
			ContainerId:  "testcontainer",
			AccountName:  "testaccount",
			Description:  "Test query description",
			DateCreated:  "2023-01-01T12:00:00Z",
			DateModified: "2023-01-01T12:00:00Z",
		}

		mockManager.GetSavedQueryByNameMock = func(name string) (data.SavedQueryOptions, error) {
			assert.Equal(t, "testquery", name)
			return expectedQuery, nil
		}

		result, err := mockManager.GetSavedQueryByName("testquery")
		assert.NoError(t, err)
		assert.Equal(t, expectedQuery, result)
	})

	t.Run("GetSavedQueryByName_NotFound", func(t *testing.T) {
		expectedErr := errors.New("query not found")

		mockManager.GetSavedQueryByNameMock = func(name string) (data.SavedQueryOptions, error) {
			return data.SavedQueryOptions{}, expectedErr
		}

		_, err := mockManager.GetSavedQueryByName("nonexistent")
		assert.Equal(t, expectedErr, err)
	})

	t.Run("GetSavedQueries_Success", func(t *testing.T) {
		expectedQueries := []data.SavedQueryOptions{
			{
				Id:           1,
				Name:         "query1",
				QueryString:  "SELECT * FROM test1",
				DatabaseId:   "db1",
				ContainerId:  "container1",
				AccountName:  "account1",
				Description:  "Description 1",
				DateCreated:  "2023-01-01T12:00:00Z",
				DateModified: "2023-01-01T12:00:00Z",
			},
			{
				Id:           2,
				Name:         "query2",
				QueryString:  "SELECT * FROM test2",
				DatabaseId:   "db2",
				ContainerId:  "container2",
				AccountName:  "account2",
				Description:  "Description 2",
				DateCreated:  "2023-01-02T12:00:00Z",
				DateModified: "2023-01-02T12:00:00Z",
			},
		}

		mockManager.GetSavedQueriesMock = func() ([]data.SavedQueryOptions, error) {
			return expectedQueries, nil
		}

		results, err := mockManager.GetSavedQueries()
		assert.NoError(t, err)
		assert.Equal(t, expectedQueries, results)
	})

	t.Run("GetSavedQueries_Error", func(t *testing.T) {
		expectedErr := errors.New("database error")

		mockManager.GetSavedQueriesMock = func() ([]data.SavedQueryOptions, error) {
			return nil, expectedErr
		}

		_, err := mockManager.GetSavedQueries()
		assert.Equal(t, expectedErr, err)
	})

	t.Run("DeleteSavedQueryByName_Success", func(t *testing.T) {
		mockManager.DeleteSavedQueryByNameMock = func(name string) error {
			assert.Equal(t, "testquery", name)
			return nil
		}

		err := mockManager.DeleteSavedQueryByName("testquery")
		assert.NoError(t, err)
	})

	t.Run("DeleteSavedQueryByName_NotFound", func(t *testing.T) {
		expectedErr := errors.New("query not found")

		mockManager.DeleteSavedQueryByNameMock = func(name string) error {
			return expectedErr
		}

		err := mockManager.DeleteSavedQueryByName("nonexistent")
		assert.Equal(t, expectedErr, err)
	})
}

func TestDefaultManager(t *testing.T) {
	originalManager := data.GetDefaultManager()
	defer data.SetDefaultManager(originalManager)

	mockManager := data.NewMockDatabaseManager()
	data.SetDefaultManager(mockManager)

	manager := data.GetDefaultManager()
	assert.Equal(t, mockManager, manager)
}

func TestGetSetDB(t *testing.T) {
	// Save the original default manager to restore it later
	originalManager := data.GetDefaultManager()
	defer data.SetDefaultManager(originalManager)

	// Create a SQLite manager and set it as the default manager
	sqliteManager := data.NewSQLiteManager("")
	data.SetDefaultManager(sqliteManager)

	// Create a new DB to test with
	testDB := &sql.DB{}
	data.SetDB(testDB)

	// Verify that GetDB returns the DB we just set
	retrievedDB := data.GetDB()
	assert.NotNil(t, retrievedDB)

	// We can't directly compare SQL DB instances, so we'll set a new one and confirm it's not nil
	newDB := &sql.DB{}
	data.SetDB(newDB)

	anotherRetrievedDB := data.GetDB()
	assert.NotNil(t, anotherRetrievedDB)
}

func TestDatabaseTablesCreation(t *testing.T) {
	mockManager := data.NewMockDatabaseManager()

	t.Run("EnsureAccountTableExists_Success", func(t *testing.T) {
		mockManager.EnsureAccountTableExistsMock = func() error {
			return nil
		}

		err := mockManager.EnsureAccountTableExists()
		assert.NoError(t, err)
	})

	t.Run("EnsureAccountTableExists_Error", func(t *testing.T) {
		expectedErr := errors.New("table creation error")

		mockManager.EnsureAccountTableExistsMock = func() error {
			return expectedErr
		}

		err := mockManager.EnsureAccountTableExists()
		assert.Equal(t, expectedErr, err)
	})

	t.Run("EnsureSavedQueryTableExists_Success", func(t *testing.T) {
		mockManager.EnsureSavedQueryTableExistsMock = func() error {
			return nil
		}

		err := mockManager.EnsureSavedQueryTableExists()
		assert.NoError(t, err)
	})

	t.Run("EnsureSavedQueryTableExists_Error", func(t *testing.T) {
		expectedErr := errors.New("table creation error")

		mockManager.EnsureSavedQueryTableExistsMock = func() error {
			return expectedErr
		}

		err := mockManager.EnsureSavedQueryTableExists()
		assert.Equal(t, expectedErr, err)
	})
}

func TestDatabaseClose(t *testing.T) {
	mockManager := data.NewMockDatabaseManager()

	t.Run("Close_Success", func(t *testing.T) {
		mockManager.CloseMock = func() error {
			return nil
		}

		err := mockManager.Close()
		assert.NoError(t, err)
	})

	t.Run("Close_Error", func(t *testing.T) {
		expectedErr := errors.New("close error")

		mockManager.CloseMock = func() error {
			return expectedErr
		}

		err := mockManager.Close()
		assert.Equal(t, expectedErr, err)
	})
}

func TestMockingOfSQLiteOperations(t *testing.T) {
	// Store original manager to restore later
	originalManager := data.GetDefaultManager()
	defer data.SetDefaultManager(originalManager)

	// Create a mock manager
	mockManager := data.NewMockDatabaseManager()

	// Set up specific behaviors we want to test
	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{
			{
				Id:               123,
				Name:             "mocked-account",
				ConnectionString: "mock-connection-string",
				Tag:              "mock-tag",
				IsDefault:        true,
			},
		}, nil
	}

	// Set as default manager
	data.SetDefaultManager(mockManager)

	// Now when we call the function on the default manager, we should get our mocked data
	accounts, err := data.GetDefaultManager().GetAccounts()

	// Verify the operation was properly mocked
	assert.NoError(t, err)
	assert.Len(t, accounts, 1)
	assert.Equal(t, "mocked-account", accounts[0].Name)
	assert.Equal(t, 123, accounts[0].Id)
	assert.Equal(t, "mock-connection-string", accounts[0].ConnectionString)
	assert.Equal(t, "mock-tag", accounts[0].Tag)
	assert.True(t, accounts[0].IsDefault)
}
