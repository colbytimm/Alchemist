package cmd

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/cosmos"
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

var databaseTableStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

// View types
type viewMode int

const (
	databaseView viewMode = iota
	containerView
)

type databaseModel struct {
	table            table.Model
	containerTable   table.Model
	account          data.AccountOptions
	databases        []string
	dbProperties     []*cosmos.DatabaseInfo
	containers       []*containerInfo
	ready            bool
	err              error
	currentView      viewMode
	selectedDatabase string
}

type containerInfo struct {
	id           string
	partitionKey string
	indexingMode string
}

type loadDatabasesMsg struct {
	account      data.AccountOptions
	databases    []string
	dbProperties []*cosmos.DatabaseInfo
	err          error
}

type loadContainersMsg struct {
	containers []*containerInfo
	err        error
}

func getContainerCount(databaseId string) int {
	containers := cosmos.GetContainerIds(databaseId)
	return len(containers)
}

func loadDatabases(account data.AccountOptions) tea.Cmd {
	return func() tea.Msg {
		var databases []string
		var dbProperties []*cosmos.DatabaseInfo

		cosmos.Connect(account.ConnectionString)

		databases = cosmos.GetDatabaseIds()

		for _, dbId := range databases {
			props := cosmos.GetDatabaseProperties(dbId)
			containerCount := getContainerCount(dbId)

			var etagStr string
			if props.ETag != nil {
				etagStr = fmt.Sprintf("%v", props.ETag)
			}

			dbInfo := &cosmos.DatabaseInfo{
				ID:             dbId,
				ResourceID:     props.ResourceID,
				SelfLink:       props.SelfLink,
				ETag:           etagStr,
				ContainerCount: containerCount,
			}

			dbProperties = append(dbProperties, dbInfo)
		}

		return loadDatabasesMsg{
			account:      account,
			databases:    databases,
			dbProperties: dbProperties,
		}
	}
}

func loadContainers(databaseId string) tea.Cmd {
	return func() tea.Msg {
		var containers []*containerInfo

		containerIds := cosmos.GetContainerIds(databaseId)

		for _, containerId := range containerIds {
			props := cosmos.GetContainerProperties(databaseId, containerId)

			partitionKey := "None"
			if len(props.PartitionKeyDefinition.Paths) > 0 {
				partitionKey = props.PartitionKeyDefinition.Paths[0]
			}

			indexingMode := "Unknown"
			if props.IndexingPolicy != nil {
				indexingMode = string(props.IndexingPolicy.IndexingMode)
			}

			container := &containerInfo{
				id:           containerId,
				partitionKey: partitionKey,
				indexingMode: indexingMode,
			}

			containers = append(containers, container)
		}

		return loadContainersMsg{
			containers: containers,
		}
	}
}

func (m databaseModel) Init() tea.Cmd {
	return loadDatabases(m.account)
}

func (m databaseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			if m.currentView == containerView {
				m.currentView = databaseView
				return m, nil
			}
			return m, tea.Quit
		case "r":
			if m.currentView == databaseView {
				return m, loadDatabases(m.account)
			}
			return m, nil
		case "enter":
			if m.currentView == databaseView && len(m.dbProperties) > 0 {
				selectedRow := m.table.SelectedRow()
				if len(selectedRow) > 0 {
					m.selectedDatabase = selectedRow[0]
					m.currentView = containerView
					return m, loadContainers(m.selectedDatabase)
				}
			}
		case "backspace":
			if m.currentView == containerView {
				m.currentView = databaseView
				return m, nil
			}
		}

	case loadDatabasesMsg:
		m.databases = msg.databases
		m.dbProperties = msg.dbProperties
		m.err = msg.err
		m.ready = true

		m.table = createDatabaseTable(m.dbProperties)

	case loadContainersMsg:
		m.containers = msg.containers
		m.containerTable = createContainerTable(m.containers)
	}

	// Handle table updates
	var cmd tea.Cmd
	if m.currentView == databaseView {
		m.table, cmd = m.table.Update(msg)
	} else {
		m.containerTable, cmd = m.containerTable.Update(msg)
	}

	return m, cmd
}

func (m databaseModel) View() string {
	if m.err != nil {
		log.Error("Error in database model", "error", m.err)
		return fmt.Sprintf("Error: %v\nPress any key to exit.", m.err)
	}

	if !m.ready {
		return "Loading databases...\nPlease wait."
	}

	var tableView string
	var helpSection string
	var header string

	if m.currentView == databaseView {
		header = fmt.Sprintf("Databases in account: %s", m.account.Name)
		tableView = databaseTableStyle.Render(m.table.View())
		helpSection = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Width(100).
			Render("Help:\n" +
				"  enter: View containers in selected database\n" +
				"  r: Refresh database list\n" +
				"  q or esc: Quit\n")
	} else {
		header = fmt.Sprintf("Containers in database: %s", m.selectedDatabase)
		tableView = databaseTableStyle.Render(m.containerTable.View())
		helpSection = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Width(100).
			Render("Help:\n" +
				"  backspace or esc: Return to database list\n" +
				"  q: Quit\n")
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Render(header),
		tableView,
		helpSection,
	)
}

func createDatabaseTable(databases []*cosmos.DatabaseInfo) table.Model {
	columns := []table.Column{
		{Title: "Database ID", Width: 40},
		{Title: "Containers", Width: 12},
	}

	var rows []table.Row
	for _, db := range databases {
		rows = append(rows, table.Row{
			db.ID,
			fmt.Sprintf("%d", db.ContainerCount),
		})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return t
}

// Helper function to create a table for containers
func createContainerTable(containers []*containerInfo) table.Model {
	columns := []table.Column{
		{Title: "Container ID", Width: 30},
		{Title: "Partition Key", Width: 30},
		{Title: "Indexing Mode", Width: 15},
	}

	var rows []table.Row
	for _, container := range containers {
		rows = append(rows, table.Row{
			container.id,
			container.partitionKey,
			container.indexingMode,
		})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return t
}

func AccountDetailsCmd() *cobra.Command {
	var (
		accountName string
		verbose     bool
	)

	cmd := &cobra.Command{
		Use:                   "account-details",
		Short:                 "Display databases in a Cosmos DB account",
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			// Configure logger
			log.SetReportTimestamp(false)

			// Set log level based on verbose flag
			if verbose {
				log.SetLevel(log.DebugLevel)
				log.Debug("Debug logging enabled")
			} else {
				log.SetLevel(log.InfoLevel)
			}

			err := data.OpenDatabase()
			if err != nil {
				log.Error("Error opening database", "error", err)
				log.Info("Make sure you've added at least one account with 'alchemist add-account'")
				return
			}

			accounts, err := data.GetAccounts()
			if err != nil {
				log.Error("Error getting accounts", "error", err)
				return
			}

			if len(accounts) == 0 {
				log.Info("No accounts found. Add an account using 'alchemist add-account'")
				return
			}

			var account data.AccountOptions

			if accountName == "" {
				defaultFound := false
				for _, acc := range accounts {
					if acc.IsDefault {
						account = acc
						defaultFound = true
						break
					}
				}

				if !defaultFound {
					account = accounts[0]
					log.Warn("No default account found. Using the first available account.")
				}
			} else {
				found := false
				for _, acc := range accounts {
					if acc.Name == accountName {
						account = acc
						found = true
						break
					}
				}

				if !found {
					log.Error("Account not found", "name", accountName)
					return
				}
			}

			log.Debug("Using account", "name", account.Name)

			model := databaseModel{
				account:     account,
				currentView: databaseView,
			}

			p := tea.NewProgram(model)
			if _, err := p.Run(); err != nil {
				log.Fatal("Error running program", "error", err)
			}
		},
	}

	cmd.Flags().StringVarP(&accountName, "account", "a", "", "Account name to use (uses default if not specified)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output with debug logging")

	return cmd
}
