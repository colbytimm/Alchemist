package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
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
	createDatabaseView
	createContainerView
	configureContainersView
)

type databaseModel struct {
	table                table.Model
	containerTable       table.Model
	account              data.AccountOptions
	databases            []string
	dbProperties         []*cosmos.DatabaseInfo
	containers           []*containerInfo
	ready                bool
	err                  error
	currentView          viewMode
	selectedDatabase     string
	databaseIDInput      textinput.Model
	containerIDInputs    []textinput.Model
	containerCount       int
	partitionKeyInputs   []textinput.Model
	activeInputIndex     int
	containerCreateError string
	databaseCreateError  string
	newDatabaseID        string
	spinner              spinner.Model
	loadingText          string
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

type createDatabaseMsg struct {
	database *cosmos.DatabaseInfo
	err      error
}

type createContainerMsg struct {
	container *containerInfo
	err       error
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

func createDatabase(databaseId string) tea.Cmd {
	return func() tea.Msg {
		dbProps, err := cosmos.CreateDatabase(databaseId)
		if err != nil {
			return createDatabaseMsg{
				database: nil,
				err:      err,
			}
		}

		dbInfo := &cosmos.DatabaseInfo{
			ID:             dbProps.ID,
			ResourceID:     dbProps.ResourceID,
			SelfLink:       dbProps.SelfLink,
			ETag:           fmt.Sprintf("%v", dbProps.ETag),
			ContainerCount: 0,
		}

		return createDatabaseMsg{
			database: dbInfo,
			err:      nil,
		}
	}
}

func createContainer(databaseId, containerId, partitionKeyPath string) tea.Cmd {
	return func() tea.Msg {
		if databaseId == "" {
			return createContainerMsg{
				container: nil,
				err:       fmt.Errorf("database ID cannot be empty"),
			}
		}

		if containerId == "" {
			return createContainerMsg{
				container: nil,
				err:       fmt.Errorf("container ID cannot be empty"),
			}
		}

		containerProps, err := cosmos.CreateContainer(databaseId, containerId, partitionKeyPath)
		if err != nil {
			return createContainerMsg{
				container: nil,
				err:       err,
			}
		}

		partitionKey := "None"
		if len(containerProps.PartitionKeyDefinition.Paths) > 0 {
			partitionKey = containerProps.PartitionKeyDefinition.Paths[0]
		}

		indexingMode := "Unknown"
		if containerProps.IndexingPolicy != nil {
			indexingMode = string(containerProps.IndexingPolicy.IndexingMode)
		}

		containerInfo := &containerInfo{
			id:           containerProps.ID,
			partitionKey: partitionKey,
			indexingMode: indexingMode,
		}

		return createContainerMsg{
			container: containerInfo,
			err:       nil,
		}
	}
}

func validateResourceID(s string) error {
	illegalChars := []rune{'/', '\\', '?', '#'}
	for _, char := range s {
		for _, illegal := range illegalChars {
			if char == illegal {
				return fmt.Errorf("ID cannot contain: /, \\, ?, #")
			}
		}
	}
	return nil
}

func initDatabaseInput() textinput.Model {
	input := textinput.New()
	input.Placeholder = "Enter database ID"
	input.Focus()
	input.CharLimit = 50
	input.Width = 30

	input.Validate = validateResourceID

	return input
}

func initContainerInputs(count int) ([]textinput.Model, []textinput.Model) {
	containerInputs := make([]textinput.Model, count)
	partitionKeyInputs := make([]textinput.Model, count)

	for i := 0; i < count; i++ {
		containerID := textinput.New()
		containerID.Placeholder = fmt.Sprintf("Container %d ID", i+1)
		containerID.CharLimit = 50
		containerID.Width = 30
		if i == 0 {
			containerID.Focus()
		}

		containerID.Validate = validateResourceID

		containerInputs[i] = containerID

		// TODO: Possibly implement default partition key and validate char limit
		partitionKey := textinput.New()
		partitionKey.Placeholder = fmt.Sprintf("Partition key for container %d (e.g., /id)", i+1)
		partitionKey.CharLimit = 50
		partitionKey.Width = 30
		partitionKeyInputs[i] = partitionKey
	}

	return containerInputs, partitionKeyInputs
}

func (m databaseModel) Init() tea.Cmd {
	// Init spinner when loading databases
	cmds := []tea.Cmd{
		m.spinner.Tick,
		loadDatabases(m.account),
	}
	return tea.Batch(cmds...)
}

func (m databaseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !m.ready {
		var spinnerCmd tea.Cmd
		m.spinner, spinnerCmd = m.spinner.Update(msg)

		if spinnerCmd != nil && !m.ready {
			return m, spinnerCmd
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.currentView == createDatabaseView {
			switch msg.String() {
			case "esc":
				m.currentView = databaseView
				return m, nil
			case "enter":
				if m.databaseIDInput.Value() == "" {
					m.databaseCreateError = "Database ID cannot be empty"
					return m, nil
				}

				err := validateResourceID(m.databaseIDInput.Value())
				if err != nil {
					m.databaseCreateError = err.Error()
					return m, nil
				}

				m.newDatabaseID = m.databaseIDInput.Value()
				m.currentView = configureContainersView
				m.containerCount = 1
				m.containerIDInputs, m.partitionKeyInputs = initContainerInputs(m.containerCount)
				m.activeInputIndex = 0
				return m, nil
			default:
				var cmd tea.Cmd
				m.databaseIDInput, cmd = m.databaseIDInput.Update(msg)
				return m, cmd
			}
		} else if m.currentView == configureContainersView || m.currentView == createContainerView {
			switch msg.String() {
			case "esc":
				if m.currentView == configureContainersView {
					m.currentView = createDatabaseView
				} else { // createContainerView
					m.currentView = containerView
				}
				return m, nil
			case "enter":
				if m.currentView == configureContainersView {
					validInputs := true
					for i := 0; i < m.containerCount; i++ {
						if m.containerIDInputs[i].Value() == "" {
							m.databaseCreateError = fmt.Sprintf("Container %d ID cannot be empty", i+1)
							validInputs = false
							break
						}
						if m.partitionKeyInputs[i].Value() == "" {
							m.databaseCreateError = fmt.Sprintf("Partition key for container %d cannot be empty", i+1)
							validInputs = false
							break
						}
					}

					if !validInputs {
						return m, nil
					}

					m.databaseCreateError = ""
					return m, createDatabase(m.newDatabaseID)
				} else { // createContainerView
					if m.selectedDatabase == "" {
						m.containerCreateError = "No database selected"
						return m, nil
					}

					validInputs := true
					for i := 0; i < m.containerCount; i++ {
						if m.containerIDInputs[i].Value() == "" {
							m.containerCreateError = fmt.Sprintf("Container %d ID cannot be empty", i+1)
							validInputs = false
							break
						}
						if m.partitionKeyInputs[i].Value() == "" {
							m.containerCreateError = fmt.Sprintf("Partition key for container %d cannot be empty", i+1)
							validInputs = false
							break
						}
					}

					if !validInputs {
						return m, nil
					}

					m.containerCreateError = ""
					containerID := m.containerIDInputs[0].Value()
					partitionKey := m.partitionKeyInputs[0].Value()

					if !strings.HasPrefix(partitionKey, "/") {
						partitionKey = "/" + partitionKey
					}

					return m, createContainer(m.selectedDatabase, containerID, partitionKey)
				}
			case "tab":
				totalInputs := m.containerCount * 2 // (container ID + partition key) * count
				m.activeInputIndex = (m.activeInputIndex + 1) % totalInputs

				if m.activeInputIndex < m.containerCount {
					for i := 0; i < m.containerCount; i++ {
						if i == m.activeInputIndex {
							m.containerIDInputs[i].Focus()
						} else {
							m.containerIDInputs[i].Blur()
						}
						m.partitionKeyInputs[i].Blur()
					}
				} else {
					partKeyIndex := m.activeInputIndex - m.containerCount
					for i := 0; i < m.containerCount; i++ {
						m.containerIDInputs[i].Blur()
						if i == partKeyIndex {
							m.partitionKeyInputs[i].Focus()
						} else {
							m.partitionKeyInputs[i].Blur()
						}
					}
				}
				return m, nil
			case "ctrl+a":
				if m.containerCount < 5 {
					m.containerCount++
					currentValues := make([]string, len(m.containerIDInputs))
					currentPartitionKeys := make([]string, len(m.partitionKeyInputs))

					for i := 0; i < len(m.containerIDInputs); i++ {
						currentValues[i] = m.containerIDInputs[i].Value()
						currentPartitionKeys[i] = m.partitionKeyInputs[i].Value()
					}

					m.containerIDInputs, m.partitionKeyInputs = initContainerInputs(m.containerCount)

					for i := 0; i < len(currentValues); i++ {
						m.containerIDInputs[i].SetValue(currentValues[i])
						m.partitionKeyInputs[i].SetValue(currentPartitionKeys[i])
					}
				}
				return m, nil
			case "ctrl+d":
				if m.containerCount > 1 {
					m.containerCount--
					currentValues := make([]string, m.containerCount)
					currentPartitionKeys := make([]string, m.containerCount)

					for i := 0; i < m.containerCount; i++ {
						currentValues[i] = m.containerIDInputs[i].Value()
						currentPartitionKeys[i] = m.partitionKeyInputs[i].Value()
					}

					m.containerIDInputs, m.partitionKeyInputs = initContainerInputs(m.containerCount)

					for i := 0; i < m.containerCount; i++ {
						m.containerIDInputs[i].SetValue(currentValues[i])
						m.partitionKeyInputs[i].SetValue(currentPartitionKeys[i])
					}
				}
				return m, nil
			default:
				var cmd tea.Cmd

				for i := 0; i < m.containerCount; i++ {
					var inputCmd tea.Cmd
					m.containerIDInputs[i], inputCmd = m.containerIDInputs[i].Update(msg)
					if inputCmd != nil {
						cmd = tea.Batch(cmd, inputCmd)
					}

					m.partitionKeyInputs[i], inputCmd = m.partitionKeyInputs[i].Update(msg)
					if inputCmd != nil {
						cmd = tea.Batch(cmd, inputCmd)
					}
				}

				return m, cmd
			}
		}

		// For all other views, handle key commands normally
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			if m.currentView == containerView {
				m.currentView = databaseView
			} else if m.currentView == configureContainersView {
				m.currentView = createDatabaseView
			} else if m.currentView == createContainerView {
				m.currentView = containerView
			} else if m.currentView == createDatabaseView {
				m.currentView = databaseView
				return m, nil
			}
			return m, tea.Quit
		case "r":
			if m.currentView == databaseView {
				return m, loadDatabases(m.account)
			}
			return m, nil
		case "c":
			if m.currentView == databaseView {
				m.currentView = createDatabaseView
				m.databaseIDInput = initDatabaseInput()
				m.databaseCreateError = ""
				return m, nil
			}
			return m, nil
		case "a":
			if m.currentView == containerView {
				m.currentView = createContainerView
				m.containerCount = 1
				m.containerIDInputs, m.partitionKeyInputs = initContainerInputs(m.containerCount)
				m.activeInputIndex = 0
				m.containerCreateError = ""
				return m, nil
			}
			return m, nil
		case "enter":
			if m.currentView == databaseView && len(m.dbProperties) > 0 {
				selectedRow := m.table.SelectedRow()
				if len(selectedRow) > 0 {
					m.selectedDatabase = selectedRow[0]
					m.currentView = containerView
					m.ready = false // Set loading state
					m.loadingText = fmt.Sprintf("Loading containers from database '%s'... Please wait.", selectedRow[0])
					return m, tea.Batch(
						m.spinner.Tick,
						loadContainers(m.selectedDatabase),
					)
				}
			}
		}
	}

	switch msg := msg.(type) {
	case loadDatabasesMsg:
		m.databases = msg.databases
		m.dbProperties = msg.dbProperties
		m.err = msg.err
		m.ready = true

		m.table = createDatabaseTable(m.dbProperties)
		return m, nil

	case loadContainersMsg:
		m.containers = msg.containers
		m.containerTable = createContainerTable(m.containers)
		m.ready = true
		return m, nil

	case createDatabaseMsg:
		if msg.err != nil {
			m.databaseCreateError = fmt.Sprintf("Error creating database: %v", msg.err)
			return m, nil
		}

		m.selectedDatabase = m.newDatabaseID

		var cmds []tea.Cmd

		for i := 0; i < m.containerCount; i++ {
			containerID := m.containerIDInputs[i].Value()
			partitionKey := m.partitionKeyInputs[i].Value()

			if !strings.HasPrefix(partitionKey, "/") {
				partitionKey = "/" + partitionKey
			}

			cmds = append(cmds, createContainer(m.newDatabaseID, containerID, partitionKey))
		}

		m.currentView = databaseView
		return m, tea.Batch(append(cmds, loadDatabases(m.account))...)

	case createContainerMsg:
		if msg.err != nil {
			m.containerCreateError = fmt.Sprintf("Error creating container: %v", msg.err)
			return m, nil
		}

		if m.activeInputIndex < m.containerCount-1 && m.currentView == configureContainersView {
			m.activeInputIndex++
			containerID := m.containerIDInputs[m.activeInputIndex].Value()
			partitionKey := m.partitionKeyInputs[m.activeInputIndex].Value()

			if !strings.HasPrefix(partitionKey, "/") {
				partitionKey = "/" + partitionKey
			}

			return m, createContainer(m.newDatabaseID, containerID, partitionKey)

		} else if m.activeInputIndex < m.containerCount-1 && m.currentView == createContainerView {
			m.activeInputIndex++
			containerID := m.containerIDInputs[m.activeInputIndex].Value()
			partitionKey := m.partitionKeyInputs[m.activeInputIndex].Value()

			if !strings.HasPrefix(partitionKey, "/") {
				partitionKey = "/" + partitionKey
			}

			return m, createContainer(m.selectedDatabase, containerID, partitionKey)
		}

		m.currentView = containerView
		return m, loadContainers(m.selectedDatabase)
	}

	var cmd tea.Cmd
	if m.currentView == databaseView {
		m.table, cmd = m.table.Update(msg)
	} else if m.currentView == containerView {
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
		return fmt.Sprintf("%s %s\n", m.spinner.View(), m.loadingText)
	}

	switch m.currentView {
	case databaseView:
		helpText := "\nPress r to refresh | c to create new database | q to quit"
		if len(m.dbProperties) == 0 {
			return fmt.Sprintf("No databases found in this account.\n%s", helpText)
		}
		return databaseTableStyle.Render(m.table.View()) + helpText

	case containerView:
		helpText := fmt.Sprintf("\nViewing containers in database: %s\nPress a to add container | esc to go back | q to quit", m.selectedDatabase)
		if len(m.containers) == 0 {
			return fmt.Sprintf("No containers found in database %s.%s", m.selectedDatabase, helpText)
		}
		return databaseTableStyle.Render(m.containerTable.View()) + helpText

	case createDatabaseView:
		s := "Create New Database\n\n"
		s += fmt.Sprintf("Database ID: %s\n", m.databaseIDInput.View())
		s += "\nIllegal characters: /, \\, ?, #\n"

		if m.databaseCreateError != "" {
			s += fmt.Sprintf("\nError: %s\n", m.databaseCreateError)
		}

		s += "\nControls:\n"
		s += "Enter: Continue to configure containers\n"
		s += "Esc: Cancel and go back\n"

		return s

	case configureContainersView:
		s := fmt.Sprintf("Configure Containers for Database: %s\n\n", m.newDatabaseID)
		s += "Add containers (up to 5):\n"
		s += "\nIllegal characters for container IDs: /, \\, ?, #\n"
		s += "Note: Dashes (-) are allowed in container IDs\n"

		for i := 0; i < m.containerCount; i++ {
			s += fmt.Sprintf("\nContainer %d ID: %s\n", i+1, m.containerIDInputs[i].View())
			s += fmt.Sprintf("Partition Key: %s\n", m.partitionKeyInputs[i].View())
		}

		if m.databaseCreateError != "" {
			s += fmt.Sprintf("\nError: %s\n", m.databaseCreateError)
		}

		s += "\nControls:\n"
		s += "Tab: Switch between inputs\n"
		s += "Ctrl+A: Add another container (max 5)\n"
		s += "Ctrl+D: Remove a container (min 1)\n"
		s += "Enter: Create database with containers\n"
		s += "Esc: Go back to database ID input\n"

		return s

	case createContainerView:
		s := fmt.Sprintf("Create New Container(s) in %s\n\n", m.selectedDatabase)
		s += "\nIllegal characters for container IDs: /, \\, ?, #\n"
		s += "Note: Dashes (-) are allowed in container IDs\n"

		for i := 0; i < m.containerCount; i++ {
			s += fmt.Sprintf("\nContainer %d ID: %s\n", i+1, m.containerIDInputs[i].View())
			s += fmt.Sprintf("Partition Key: %s\n", m.partitionKeyInputs[i].View())
		}

		if m.containerCreateError != "" {
			s += fmt.Sprintf("\nError: %s\n", m.containerCreateError)
		}

		s += "\nControls:\n"
		s += "Tab: Switch between inputs\n"
		s += "Ctrl+A: Add another container (max 5)\n"
		s += "Ctrl+D: Remove a container (min 1)\n"
		s += "Enter: Create container(s)\n"
		s += "Esc: Cancel and go back\n"

		return s
	}

	return "Unknown view"
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
		Short:                 "Display details about all databases and containers in a Cosmos DB account",
		Args:                  cobra.ExactArgs(0),
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

			s := spinner.New()
			s.Spinner = spinner.Dot
			s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

			model := databaseModel{
				account:     account,
				currentView: databaseView,
				spinner:     s,
				loadingText: "Loading databases from account... Please wait.",
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
