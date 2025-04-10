package test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/colbytimm/alchemist/cmd"
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/stretchr/testify/assert"
)

// Store original implementations to restore after tests.
var (
	cosmosConnectImpl                = cosmos.ConnectImpl
	cosmosGetDatabaseIDsImpl         = cosmos.GetDatabaseIDsImpl
	cosmosGetDatabasePropertiesImpl  = cosmos.GetDatabasePropertiesImpl
	cosmosGetContainerIDsImpl        = cosmos.GetContainerIDsImpl
	cosmosGetContainerPropertiesImpl = cosmos.GetContainerPropertiesImpl
	cosmosCreateDatabaseImpl         = cosmos.CreateDatabaseImpl
	cosmosCreateContainerImpl        = cosmos.CreateContainerImpl
)

func setupTest() {
	cosmos.ConnectImpl = func(cosmosConnectionString string) error {
		if cosmosConnectionString == "" {
			return fmt.Errorf("missing Cosmos Connection String")
		}
		return nil
	}

	cosmos.GetDatabaseIDsImpl = func() []string {
		return []string{"db1", "db2"}
	}

	cosmos.GetDatabasePropertiesImpl = func(dbID string) *azcosmos.DatabaseProperties {
		etag := azcore.ETag("etag-" + dbID)
		return &azcosmos.DatabaseProperties{
			ID:         dbID,
			ResourceID: "rid-" + dbID,
			SelfLink:   "self-" + dbID,
			ETag:       &etag,
		}
	}

	cosmos.GetContainerIDsImpl = func(dbID string) []string {
		return []string{"container1", "container2", "newcontainer"}
	}

	cosmos.GetContainerPropertiesImpl = func(dbID, containerID string) *azcosmos.ContainerProperties {
		return &azcosmos.ContainerProperties{
			ID: containerID,
			PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{
				Paths: []string{"/id"},
			},
			IndexingPolicy: &azcosmos.IndexingPolicy{
				IndexingMode: azcosmos.IndexingMode("consistent"),
			},
		}
	}

	cosmos.CreateDatabaseImpl = func(databaseID string) (*azcosmos.DatabaseProperties, error) {
		etag := azcore.ETag("etag-" + databaseID)
		return &azcosmos.DatabaseProperties{
			ID:         databaseID,
			ResourceID: "rid-" + databaseID,
			SelfLink:   "self-" + databaseID,
			ETag:       &etag,
		}, nil
	}

	cosmos.CreateContainerImpl = func(databaseID, containerID, partitionKeyPath string) (*azcosmos.ContainerProperties, error) {
		return &azcosmos.ContainerProperties{
			ID: containerID,
			PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{
				Paths: []string{partitionKeyPath},
			},
			IndexingPolicy: &azcosmos.IndexingPolicy{
				IndexingMode: azcosmos.IndexingMode("consistent"),
			},
		}, nil
	}
}

func teardownTest() {
	cosmos.ConnectImpl = cosmosConnectImpl
	cosmos.GetDatabaseIDsImpl = cosmosGetDatabaseIDsImpl
	cosmos.GetDatabasePropertiesImpl = cosmosGetDatabasePropertiesImpl
	cosmos.GetContainerIDsImpl = cosmosGetContainerIDsImpl
	cosmos.GetContainerPropertiesImpl = cosmosGetContainerPropertiesImpl
	cosmos.CreateDatabaseImpl = cosmosCreateDatabaseImpl
	cosmos.CreateContainerImpl = cosmosCreateContainerImpl
}

func TestGetAccountDetails_DatabaseError(t *testing.T) {
	mockManager.OpenDatabaseMock = func() error {
		return errors.New("database error")
	}

	_, err := cmd.GetAccountDetailsInternal("", mockManager)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

func TestGetAccountDetails_NoAccounts(t *testing.T) {
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return []data.AccountOptions{}, nil
	}

	_, err := cmd.GetAccountDetailsInternal("", mockManager)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no accounts found")
}

func TestGetAccountDetails_DefaultAccount(t *testing.T) {
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	testAccounts := []data.AccountOptions{
		{
			Id:               1,
			Name:             "regular-account",
			ConnectionString: "regular-connection-string",
			Tag:              "dev",
			IsDefault:        false,
		},
		{
			Id:               2,
			Name:             "default-account",
			ConnectionString: "default-connection-string",
			Tag:              "prod",
			IsDefault:        true,
		},
	}

	mockManager.GetAccountsMock = func() ([]data.AccountOptions, error) {
		return testAccounts, nil
	}

	account, err := cmd.GetAccountDetailsInternal("", mockManager)

	assert.NoError(t, err)
	assert.Equal(t, "default-account", account.Name)
	assert.Equal(t, true, account.IsDefault)
}

func TestGetAccountDetails_SpecificAccount(t *testing.T) {
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.GetAccountByNameMock = func(name string) (data.AccountOptions, error) {
		if name == "specific-account" {
			return data.AccountOptions{
				Id:               3,
				Name:             "specific-account",
				ConnectionString: "specific-connection-string",
				Tag:              "test",
				IsDefault:        false,
			}, nil
		}
		return data.AccountOptions{}, errors.New("account not found")
	}

	account, err := cmd.GetAccountDetailsInternal("specific-account", mockManager)

	assert.NoError(t, err)
	assert.Equal(t, "specific-account", account.Name)
	assert.Equal(t, "specific-connection-string", account.ConnectionString)
}

func TestGetAccountDetails_AccountNotFound(t *testing.T) {
	mockManager.OpenDatabaseMock = func() error {
		return nil
	}

	mockManager.GetAccountByNameMock = func(name string) (data.AccountOptions, error) {
		return data.AccountOptions{}, errors.New("account with name 'not-found' not found")
	}

	_, err := cmd.GetAccountDetailsInternal("not-found", mockManager)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestHandleLoadingState(t *testing.T) {
	setupTest()
	defer teardownTest()

	tests := []struct {
		name       string
		msg        tea.Msg
		wantErr    bool
		wantReady  bool
		setupModel func(*cmd.DatabaseModel)
		skipCmd    bool
	}{
		{
			name: "loadDatabasesMsg with error",
			msg: cmd.LoadDatabasesMsg{
				Err: fmt.Errorf("test error"),
			},
			wantErr:   true,
			wantReady: false,
		},
		{
			name: "loadDatabasesMsg without error",
			msg: cmd.LoadDatabasesMsg{
				Account: data.AccountOptions{
					ConnectionString: "test-connection-string",
				},
				Databases: []string{"db1", "db2"},
				DbProperties: []*cosmos.DatabaseInfo{
					{ID: "db1", ContainerCount: 1},
					{ID: "db2", ContainerCount: 2},
				},
			},
			wantErr:   false,
			wantReady: true,
		},
		{
			name: "loadContainersMsg",
			msg: cmd.LoadContainersMsg{
				Containers: []*cmd.ContainerInfo{
					{ID: "container1", PartitionKey: "/id", IndexingMode: "consistent"},
					{ID: "container2", PartitionKey: "/id", IndexingMode: "consistent"},
				},
			},
			wantErr:   false,
			wantReady: true,
		},
		{
			name: "createDatabaseMsg with error",
			msg: cmd.CreateDatabaseMsg{
				Err: fmt.Errorf("test error"),
			},
			wantErr:   true,
			wantReady: false,
		},
		{
			name: "createDatabaseMsg without error",
			msg: cmd.CreateDatabaseMsg{
				Database: &cosmos.DatabaseInfo{
					ID:             "newdb",
					ContainerCount: 0,
				},
			},
			wantErr:   false,
			wantReady: true,
			setupModel: func(m *cmd.DatabaseModel) {
				m.Account = data.AccountOptions{
					ConnectionString: "AccountEndpoint=https://test.documents.azure.com:443/;AccountKey=key==;",
				}
				m.ContainerCount = 1
				m.ContainerIDInputs = []textinput.Model{textinput.New()}
				m.ContainerIDInputs[0].SetValue("container1")
				m.PartitionKeyInputs = []textinput.Model{textinput.New()}
				m.PartitionKeyInputs[0].SetValue("id")
			},
		},
		{
			name: "createContainerMsg with error",
			msg: cmd.CreateContainerMsg{
				Err: fmt.Errorf("test error"),
			},
			wantErr:   true,
			wantReady: false,
		},
		{
			name: "createContainerMsg without error",
			msg: cmd.CreateContainerMsg{
				Container: &cmd.ContainerInfo{
					ID:           "newcontainer",
					PartitionKey: "/id",
					IndexingMode: "consistent",
				},
			},
			wantErr:   false,
			wantReady: true,
			setupModel: func(m *cmd.DatabaseModel) {
				m.Account = data.AccountOptions{
					ConnectionString: "AccountEndpoint=https://test.documents.azure.com:443/;AccountKey=key==;",
				}
				m.SelectedDatabase = "testdb"
				m.CurrentView = cmd.CreateContainerView
				m.ContainerCount = 1
				m.ContainerIDInputs = []textinput.Model{textinput.New()}
				m.ContainerIDInputs[0].SetValue("newcontainer")
				m.PartitionKeyInputs = []textinput.Model{textinput.New()}
				m.PartitionKeyInputs[0].SetValue("id")
				m.ActiveInputIndex = 0
				m.Containers = []*cmd.ContainerInfo{
					{ID: "container1", PartitionKey: "/id", IndexingMode: "consistent"},
					{ID: "container2", PartitionKey: "/id", IndexingMode: "consistent"},
				}
			},
			skipCmd: true,
		},
		{
			name:      "unknown message type",
			msg:       "unknown",
			wantErr:   false,
			wantReady: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &cmd.DatabaseModel{
				Spinner: spinner.New(),
				Account: data.AccountOptions{
					ConnectionString: "test-connection-string",
				},
			}

			if tt.setupModel != nil {
				tt.setupModel(m)
			}

			model, cmdFunc := m.HandleLoadingState(tt.msg)

			if tt.wantErr {
				assert.Error(t, model.(*cmd.DatabaseModel).Err)
			} else {
				assert.NoError(t, model.(*cmd.DatabaseModel).Err)
			}

			assert.Equal(t, tt.wantReady, model.(*cmd.DatabaseModel).Ready)

			if cmdFunc != nil && !tt.skipCmd {
				// Execute the command to ensure it doesn't panic
				model, _ = model.Update(cmdFunc())
				assert.NoError(t, model.(*cmd.DatabaseModel).Err)
			}
		})
	}
}
