package test

import (
	"github.com/colbytimm/alchemist/data"
)

// MockDB is a mock implementation of the data layer for testing
type MockDB struct {
	OpenDatabaseFunc             func() error
	EnsureAccountTableExistsFunc func() error
	InsertAccountFunc            func(*data.AccountOptions) (data.AccountOptions, error)
	GetAccountByNameFunc         func(string) (data.AccountOptions, error)
	GetAccountsFunc              func() ([]data.AccountOptions, error)
	DeleteAccountByNameFunc      func(string) error
	UpdateDefaultItemFunc        func(string) error
}

func (m *MockDB) OpenDatabase() error {
	if m.OpenDatabaseFunc != nil {
		return m.OpenDatabaseFunc()
	}
	return nil
}

func (m *MockDB) EnsureAccountTableExists() error {
	if m.EnsureAccountTableExistsFunc != nil {
		return m.EnsureAccountTableExistsFunc()
	}
	return nil
}

func (m *MockDB) InsertAccount(options *data.AccountOptions) (data.AccountOptions, error) {
	if m.InsertAccountFunc != nil {
		return m.InsertAccountFunc(options)
	}
	return data.AccountOptions{}, nil
}

func (m *MockDB) GetAccountByName(name string) (data.AccountOptions, error) {
	if m.GetAccountByNameFunc != nil {
		return m.GetAccountByNameFunc(name)
	}
	return data.AccountOptions{}, nil
}

func (m *MockDB) GetAccounts() ([]data.AccountOptions, error) {
	if m.GetAccountsFunc != nil {
		return m.GetAccountsFunc()
	}
	return []data.AccountOptions{}, nil
}

func (m *MockDB) DeleteAccountByName(name string) error {
	if m.DeleteAccountByNameFunc != nil {
		return m.DeleteAccountByNameFunc(name)
	}
	return nil
}

func (m *MockDB) UpdateDefaultItem(name string) error {
	if m.UpdateDefaultItemFunc != nil {
		return m.UpdateDefaultItemFunc(name)
	}
	return nil
}
