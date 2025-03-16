package test

import (
	"errors"
	"testing"

	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/data"
)

var mockDataManager *MockDB

func setup() {
	mockDataManager = &MockDB{}
	data.ActivateMock()
}

func teardown() {
	mockDataManager = nil
	data.DeactivateMock()
}

func TestAddAccountCmd_Success(t *testing.T) {
	setup()
	defer teardown()

	data.OpenDatabaseMock = func() error {
		return nil
	}

	data.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	data.InsertAccountMock = func(options *data.AccountOptions) (data.AccountOptions, error) {
		return data.AccountOptions{
			Id:               1,
			Name:             options.Name,
			ConnectionString: options.ConnectionString,
			Tag:              options.Tag,
			IsDefault:        true,
		}, nil
	}

	cmd := cmd.AddAccountCmd()

	cmd.SetArgs([]string{
		"--name", "test-account",
		"--connection", "test-connection-string",
		"--tag", "test",
	})

	output := CaptureOutput(func() {
		cmd.Execute()
	})

	AssertStringContains(t, output, "Account added successfully")
	AssertStringContains(t, output, "test-account")
}

func TestAddAccountCmd_DatabaseError(t *testing.T) {
	setup()
	defer teardown()

	data.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	cmd := cmd.AddAccountCmd()

	cmd.SetArgs([]string{
		"--name", "test-account",
		"--connection", "test-connection-string",
		"--tag", "test",
	})

	output := CaptureOutput(func() {
		cmd.Execute()
	})

	AssertStringContains(t, output, "Could not open database")
	AssertStringContains(t, output, "database error")
}

func TestAddAccountCmd_TableError(t *testing.T) {
	setup()
	defer teardown()

	data.OpenDatabaseMock = func() error {
		return nil
	}

	data.EnsureAccountTableExistsMock = func() error {
		return errors.New("table error")
	}

	cmd := cmd.AddAccountCmd()

	cmd.SetArgs([]string{
		"--name", "test-account",
		"--connection", "test-connection-string",
		"--tag", "test",
	})

	output := CaptureOutput(func() {
		cmd.Execute()
	})

	AssertStringContains(t, output, "Could not ensure account table exists")
	AssertStringContains(t, output, "table error")
}

func TestAddAccountCmd_InsertError(t *testing.T) {
	setup()
	defer teardown()

	data.OpenDatabaseMock = func() error {
		return nil
	}

	data.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	data.InsertAccountMock = func(options *data.AccountOptions) (data.AccountOptions, error) {
		return data.AccountOptions{}, errors.New("insert error")
	}

	cmd := cmd.AddAccountCmd()

	cmd.SetArgs([]string{
		"--name", "test-account",
		"--connection", "test-connection-string",
		"--tag", "test",
	})

	output := CaptureOutput(func() {
		cmd.Execute()
	})

	AssertStringContains(t, output, "Could not insert account")
	AssertStringContains(t, output, "insert error")
}

func TestAddAccountCmd_MissingRequiredFlags(t *testing.T) {
	cmd := cmd.AddAccountCmd()

	cmd.SetArgs([]string{
		"--name", "test-account",
		// Missing connection and tag
	})

	var err error

	output := CaptureOutput(func() {
		err = cmd.Execute()
	})

	AssertError(t, err)
	AssertStringContains(t, output, "required flag")
}

func TestAddAccountCmd_VerboseOutput(t *testing.T) {
	setup()
	defer teardown()

	data.OpenDatabaseMock = func() error {
		return nil
	}

	data.EnsureAccountTableExistsMock = func() error {
		return nil
	}

	data.InsertAccountMock = func(options *data.AccountOptions) (data.AccountOptions, error) {
		return data.AccountOptions{
			Id:               1,
			Name:             options.Name,
			ConnectionString: options.ConnectionString,
			Tag:              options.Tag,
			IsDefault:        true,
		}, nil
	}

	cmd := cmd.AddAccountCmd()

	cmd.SetArgs([]string{
		"--name", "test-account",
		"--connection", "test-connection-string",
		"--tag", "test",
		"--verbose",
	})

	output := CaptureOutput(func() {
		cmd.Execute()
	})

	AssertStringContains(t, output, "Debug logging enabled")
	AssertStringContains(t, output, "Account added successfully")
}
