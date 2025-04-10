package cmd

import (
	"errors"
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
	"github.com/colbytimm/alchemist/services"
	"github.com/colbytimm/alchemist/util"
	"github.com/spf13/cobra"
)

func GetAccountDetailsInternal(accountName string, dbManager data.DatabaseManager) (data.AccountOptions, error) {
	err := dbManager.OpenDatabase()
	if err != nil {
		return data.AccountOptions{}, fmt.Errorf("could not open database: %w", err)
	}

	var account data.AccountOptions

	if accountName == "" {
		accounts, err := dbManager.GetAccounts()
		if err != nil {
			return data.AccountOptions{}, fmt.Errorf("could not retrieve accounts: %w", err)
		}

		if len(accounts) == 0 {
			return data.AccountOptions{}, errors.New("no accounts found")
		}

		for _, acc := range accounts {
			if acc.IsDefault {
				account = acc
				break
			}
		}

		if account.Id == 0 {
			account = accounts[0]
		}
	} else {
		account, err = dbManager.GetAccountByName(accountName)
		if err != nil {
			return data.AccountOptions{}, fmt.Errorf("could not retrieve account: %w", err)
		}
	}

	return account, nil
}

type ViewMode int

const (
	DatabaseView ViewMode = iota
	ContainerView
	CreateDatabaseView
	CreateContainerView
	ConfigureContainersView
)

type DatabaseModel struct {
	Table                table.Model
	ContainerTable       table.Model
	Account              data.AccountOptions
	Databases            []string
	DbProperties         []*cosmos.DatabaseInfo
	Containers           []*ContainerInfo
	Ready                bool
	Err                  error
	CurrentView          ViewMode
	SelectedDatabase     string
	DatabaseIDInput      textinput.Model
	ContainerIDInputs    []textinput.Model
	ContainerCount       int
	PartitionKeyInputs   []textinput.Model
	ActiveInputIndex     int
	ContainerCreateError string
	DatabaseCreateError  string
	NewDatabaseID        string
	Spinner              spinner.Model
	LoadingText          string
}

type ContainerInfo struct {
	ID           string
	PartitionKey string
	IndexingMode string
}

type LoadDatabasesMsg struct {
	Account      data.AccountOptions
	Databases    []string
	DbProperties []*cosmos.DatabaseInfo
	Err          error
}

type LoadContainersMsg struct {
	Containers []*ContainerInfo
}

type CreateDatabaseMsg struct {
	Database *cosmos.DatabaseInfo
	Err      error
}

type CreateContainerMsg struct {
	Container *ContainerInfo
	Err       error
}

func getContainerCount(databaseID string) int {
	containers := cosmos.GetContainerIDs(databaseID)
	return len(containers)
}

func loadDatabases(account data.AccountOptions) tea.Cmd {
	return func() tea.Msg {
		var databases []string
		var dbProperties []*cosmos.DatabaseInfo

		err := cosmos.Connect(account.ConnectionString)
		if err != nil {
			return LoadDatabasesMsg{
				Account:      account,
				Databases:    nil,
				DbProperties: nil,
				Err:          err,
			}
		}

		databases = cosmos.GetDatabaseIDs()

		for _, dbID := range databases {
			props := cosmos.GetDatabaseProperties(dbID)
			containerCount := getContainerCount(dbID)

			var etagStr string
			if props.ETag != nil {
				etagStr = fmt.Sprintf("%v", props.ETag)
			}

			dbInfo := &cosmos.DatabaseInfo{
				ID:             dbID,
				ResourceID:     props.ResourceID,
				SelfLink:       props.SelfLink,
				ETag:           etagStr,
				ContainerCount: containerCount,
			}

			dbProperties = append(dbProperties, dbInfo)
		}

		return LoadDatabasesMsg{
			Account:      account,
			Databases:    databases,
			DbProperties: dbProperties,
			Err:          nil,
		}
	}
}

func loadContainers(databaseID string) tea.Cmd {
	return func() tea.Msg {
		var containers []*ContainerInfo

		containerIDs := cosmos.GetContainerIDs(databaseID)

		for _, containerID := range containerIDs {
			props := cosmos.GetContainerProperties(databaseID, containerID)

			partitionKey := "None"
			if len(props.PartitionKeyDefinition.Paths) > 0 {
				partitionKey = props.PartitionKeyDefinition.Paths[0]
			}

			indexingMode := "Unknown"
			if props.IndexingPolicy != nil {
				indexingMode = string(props.IndexingPolicy.IndexingMode)
			}

			container := &ContainerInfo{
				ID:           containerID,
				PartitionKey: partitionKey,
				IndexingMode: indexingMode,
			}

			containers = append(containers, container)
		}

		return LoadContainersMsg{
			Containers: containers,
		}
	}
}

func createDatabase(databaseID string) tea.Cmd {
	return func() tea.Msg {
		var database *cosmos.DatabaseInfo
		var err error

		props, err := cosmos.CreateDatabase(databaseID)
		if err != nil {
			return CreateDatabaseMsg{
				Database: nil,
				Err:      fmt.Errorf("failed to create database: %w", err),
			}
		}

		etagStr := ""
		if props.ETag != nil {
			etagStr = fmt.Sprintf("%v", props.ETag)
		}

		database = &cosmos.DatabaseInfo{
			ID:             props.ID,
			ResourceID:     props.ResourceID,
			SelfLink:       props.SelfLink,
			ETag:           etagStr,
			ContainerCount: 0,
		}

		return CreateDatabaseMsg{
			Database: database,
			Err:      nil,
		}
	}
}

func createContainer(databaseID, containerID, partitionKeyPath string) tea.Cmd {
	return func() tea.Msg {
		if databaseID == "" {
			return CreateContainerMsg{
				Container: nil,
				Err:       fmt.Errorf("database ID cannot be empty"),
			}
		}

		if containerID == "" {
			return CreateContainerMsg{
				Container: nil,
				Err:       fmt.Errorf("container ID cannot be empty"),
			}
		}

		containerProps, err := cosmos.CreateContainer(databaseID, containerID, partitionKeyPath)
		if err != nil {
			return CreateContainerMsg{
				Container: nil,
				Err:       err,
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

		containerInfo := &ContainerInfo{
			ID:           containerProps.ID,
			PartitionKey: partitionKey,
			IndexingMode: indexingMode,
		}

		return CreateContainerMsg{
			Container: containerInfo,
			Err:       nil,
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

func initContainerInputs(count int) (containerInputs, partitionKeyInputs []textinput.Model) {
	containerInputs = make([]textinput.Model, count)
	partitionKeyInputs = make([]textinput.Model, count)

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

		// TODO: Possibly implement default partition key and validate char limit.
		partitionKey := textinput.New()
		partitionKey.Placeholder = fmt.Sprintf("Partition key for container %d (e.g., /id)", i+1)
		partitionKey.CharLimit = 50
		partitionKey.Width = 30
		partitionKeyInputs[i] = partitionKey
	}

	return containerInputs, partitionKeyInputs
}

func (m *DatabaseModel) Init() tea.Cmd {
	// Init spinner when loading databases.
	cmds := []tea.Cmd{
		m.Spinner.Tick,
		loadDatabases(m.Account),
	}
	return tea.Batch(cmds...)
}

func (m *DatabaseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !m.Ready {
		return m.HandleLoadingState(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.HandleKeyMsg(msg)
	case LoadDatabasesMsg:
		return m.HandleLoadDatabasesMsg(&msg)
	case LoadContainersMsg:
		return m.HandleLoadContainersMsg(msg)
	case CreateDatabaseMsg:
		return m.HandleCreateDatabaseMsg(msg)
	case CreateContainerMsg:
		return m.HandleCreateContainerMsg(msg)
	default:
		return m.HandleDefaultMsg(msg)
	}
}

func (m *DatabaseModel) HandleLoadingState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case LoadDatabasesMsg:
		return m.HandleLoadDatabasesMsg(&msg)
	case LoadContainersMsg:
		return m.HandleLoadContainersMsg(msg)
	case CreateDatabaseMsg:
		return m.HandleCreateDatabaseMsg(msg)
	case CreateContainerMsg:
		return m.HandleCreateContainerMsg(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.Spinner, cmd = m.Spinner.Update(msg)
		return m, cmd
	default:
		return m, nil
	}
}

func (m *DatabaseModel) HandleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.CurrentView {
	case CreateDatabaseView:
		return m.HandleCreateDatabaseViewKeyMsg(msg)
	case ConfigureContainersView, CreateContainerView:
		return m.HandleContainerConfigViewKeyMsg(msg)
	default:
		return m.HandleCommonKeyCommands(msg)
	}
}

func (m *DatabaseModel) HandleCreateDatabaseViewKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.CurrentView = DatabaseView
		return m, nil
	case "enter":
		if m.DatabaseIDInput.Value() == "" {
			m.DatabaseCreateError = "Database ID cannot be empty"
			return m, nil
		}

		err := validateResourceID(m.DatabaseIDInput.Value())
		if err != nil {
			m.DatabaseCreateError = err.Error()
			return m, nil
		}

		m.NewDatabaseID = m.DatabaseIDInput.Value()
		m.CurrentView = ConfigureContainersView
		m.ContainerCount = 1
		m.ContainerIDInputs, m.PartitionKeyInputs = initContainerInputs(m.ContainerCount)
		m.ActiveInputIndex = 0
		return m, nil
	default:
		var cmd tea.Cmd
		m.DatabaseIDInput, cmd = m.DatabaseIDInput.Update(msg)
		return m, cmd
	}
}

func (m *DatabaseModel) HandleContainerConfigViewKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.CurrentView == ConfigureContainersView {
			m.CurrentView = CreateDatabaseView
		} else {
			m.CurrentView = DatabaseView
		}
		return m, nil
	case "enter":
		return m.HandleContainerConfigEnter()
	case "tab":
		return m.HandleContainerConfigTab()
	case "ctrl+a":
		return m.HandleAddContainer()
	case "ctrl+d":
		return m.HandleRemoveContainer()
	default:
		return m.HandleContainerInputs(msg)
	}
}

func (m *DatabaseModel) HandleContainerConfigEnter() (tea.Model, tea.Cmd) {
	if m.CurrentView == ConfigureContainersView {
		if !m.ValidateContainerInputs(&m.DatabaseCreateError) {
			return m, nil
		}

		m.DatabaseCreateError = ""
		return m, createDatabase(m.NewDatabaseID)
	}

	if m.SelectedDatabase == "" {
		m.ContainerCreateError = "No database selected"
		return m, nil
	}

	if !m.ValidateContainerInputs(&m.ContainerCreateError) {
		return m, nil
	}

	m.ContainerCreateError = ""
	containerID := m.ContainerIDInputs[0].Value()
	partitionKey := m.PartitionKeyInputs[0].Value()

	if !strings.HasPrefix(partitionKey, "/") {
		partitionKey = "/" + partitionKey
	}

	return m, createContainer(m.SelectedDatabase, containerID, partitionKey)
}

func (m *DatabaseModel) ValidateContainerInputs(errorMsg *string) bool {
	for i := 0; i < m.ContainerCount; i++ {
		if m.ContainerIDInputs[i].Value() == "" {
			*errorMsg = fmt.Sprintf("Container %d ID cannot be empty", i+1)
			return false
		}
		if m.PartitionKeyInputs[i].Value() == "" {
			*errorMsg = fmt.Sprintf("Partition key for container %d cannot be empty", i+1)
			return false
		}
	}
	return true
}

func (m *DatabaseModel) HandleContainerConfigTab() (tea.Model, tea.Cmd) {
	totalInputs := m.ContainerCount * 2
	m.ActiveInputIndex = (m.ActiveInputIndex + 1) % totalInputs

	if m.ActiveInputIndex < m.ContainerCount {
		for i := 0; i < m.ContainerCount; i++ {
			if i == m.ActiveInputIndex {
				m.ContainerIDInputs[i].Focus()
			} else {
				m.ContainerIDInputs[i].Blur()
			}
			m.PartitionKeyInputs[i].Blur()
		}
	} else {
		partKeyIndex := m.ActiveInputIndex - m.ContainerCount
		for i := 0; i < m.ContainerCount; i++ {
			m.ContainerIDInputs[i].Blur()
			if i == partKeyIndex {
				m.PartitionKeyInputs[i].Focus()
			} else {
				m.PartitionKeyInputs[i].Blur()
			}
		}
	}
	return m, nil
}

func (m *DatabaseModel) HandleAddContainer() (tea.Model, tea.Cmd) {
	if m.ContainerCount < 5 {
		m.ContainerCount++
		currentValues := make([]string, len(m.ContainerIDInputs))
		currentPartitionKeys := make([]string, len(m.PartitionKeyInputs))

		for i := 0; i < len(m.ContainerIDInputs); i++ {
			currentValues[i] = m.ContainerIDInputs[i].Value()
			currentPartitionKeys[i] = m.PartitionKeyInputs[i].Value()
		}

		m.ContainerIDInputs, m.PartitionKeyInputs = initContainerInputs(m.ContainerCount)

		for i := 0; i < len(currentValues); i++ {
			m.ContainerIDInputs[i].SetValue(currentValues[i])
			m.PartitionKeyInputs[i].SetValue(currentPartitionKeys[i])
		}
	}
	return m, nil
}

func (m *DatabaseModel) HandleRemoveContainer() (tea.Model, tea.Cmd) {
	if m.ContainerCount > 1 {
		m.ContainerCount--
		currentValues := make([]string, m.ContainerCount)
		currentPartitionKeys := make([]string, m.ContainerCount)

		for i := 0; i < m.ContainerCount; i++ {
			currentValues[i] = m.ContainerIDInputs[i].Value()
			currentPartitionKeys[i] = m.PartitionKeyInputs[i].Value()
		}

		m.ContainerIDInputs, m.PartitionKeyInputs = initContainerInputs(m.ContainerCount)

		for i := 0; i < m.ContainerCount; i++ {
			m.ContainerIDInputs[i].SetValue(currentValues[i])
			m.PartitionKeyInputs[i].SetValue(currentPartitionKeys[i])
		}
	}
	return m, nil
}

func (m *DatabaseModel) HandleContainerInputs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	for i := 0; i < m.ContainerCount; i++ {
		var inputCmd tea.Cmd
		m.ContainerIDInputs[i], inputCmd = m.ContainerIDInputs[i].Update(msg)
		if inputCmd != nil {
			cmd = tea.Batch(cmd, inputCmd)
		}

		m.PartitionKeyInputs[i], inputCmd = m.PartitionKeyInputs[i].Update(msg)
		if inputCmd != nil {
			cmd = tea.Batch(cmd, inputCmd)
		}
	}

	return m, cmd
}

func (m *DatabaseModel) HandleCommonKeyCommands(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.CurrentView == DatabaseView || m.CurrentView == ContainerView {
		switch msg.String() {
		case "up", "down", "left", "right", "pgup", "pgdown", "home", "end":
			return m.HandleDefaultMsg(msg)
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m.HandleExitKeyCommand()
	case "r":
		if m.CurrentView == DatabaseView {
			return m, loadDatabases(m.Account)
		}
		return m, nil
	case "c":
		if m.CurrentView == DatabaseView {
			m.CurrentView = CreateDatabaseView
			m.DatabaseIDInput = initDatabaseInput()
			m.DatabaseCreateError = ""
			return m, nil
		}
		return m, nil
	case "a":
		if m.CurrentView == ContainerView {
			m.CurrentView = CreateContainerView
			m.ContainerCount = 1
			m.ContainerIDInputs, m.PartitionKeyInputs = initContainerInputs(m.ContainerCount)
			m.ActiveInputIndex = 0
			m.ContainerCreateError = ""
			return m, nil
		}
		return m, nil
	case "enter":
		return m.HandleEnterKeyForListViews()
	default:
		return m, nil
	}
}

func (m *DatabaseModel) HandleExitKeyCommand() (tea.Model, tea.Cmd) {
	switch m.CurrentView {
	case ContainerView:
		m.CurrentView = DatabaseView
		return m, nil
	case ConfigureContainersView:
		m.CurrentView = CreateDatabaseView
	case CreateContainerView:
		m.CurrentView = ContainerView
	case CreateDatabaseView:
		m.CurrentView = DatabaseView
	default:
		return m, tea.Quit
	}
	return m, nil
}

func (m *DatabaseModel) HandleEnterKeyForListViews() (tea.Model, tea.Cmd) {
	if m.CurrentView == DatabaseView && len(m.DbProperties) > 0 {
		selectedRow := m.Table.SelectedRow()
		if len(selectedRow) > 0 {
			m.SelectedDatabase = selectedRow[0]
			m.CurrentView = ContainerView
			m.Ready = false
			m.LoadingText = fmt.Sprintf("Loading containers from database '%s'... Please wait.", selectedRow[0])
			return m, tea.Batch(
				m.Spinner.Tick,
				loadContainers(m.SelectedDatabase),
			)
		}
	}
	return m, nil
}

func (m *DatabaseModel) HandleLoadDatabasesMsg(msg *LoadDatabasesMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.Err = fmt.Errorf("failed to load databases: %w", msg.Err)
		return m, nil
	}

	m.Databases = msg.Databases
	m.DbProperties = msg.DbProperties
	m.Ready = true

	m.Table = createDatabaseTable(m.DbProperties)
	return m, nil
}

func (m *DatabaseModel) HandleLoadContainersMsg(msg LoadContainersMsg) (tea.Model, tea.Cmd) {
	m.Containers = msg.Containers
	m.ContainerTable = createContainerTable(m.Containers)
	m.Ready = true
	return m, nil
}

func (m *DatabaseModel) HandleCreateDatabaseMsg(msg CreateDatabaseMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.Err = fmt.Errorf("failed to create database: %w", msg.Err)
		m.Ready = false
		return m, nil
	}

	m.SelectedDatabase = m.NewDatabaseID
	m.Ready = true

	var cmds []tea.Cmd

	for i := 0; i < m.ContainerCount; i++ {
		containerID := m.ContainerIDInputs[i].Value()
		partitionKey := m.PartitionKeyInputs[i].Value()

		if !strings.HasPrefix(partitionKey, "/") {
			partitionKey = "/" + partitionKey
		}

		cmds = append(cmds, createContainer(m.NewDatabaseID, containerID, partitionKey))
	}

	m.CurrentView = DatabaseView
	return m, tea.Batch(append(cmds, loadDatabases(m.Account))...)
}

func (m *DatabaseModel) HandleCreateContainerMsg(msg CreateContainerMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.Err = fmt.Errorf("failed to create container: %w", msg.Err)
		m.Ready = false
		return m, nil
	}

	m.Ready = true

	if m.ActiveInputIndex < m.ContainerCount-1 && m.CurrentView == ConfigureContainersView {
		m.ActiveInputIndex++
		containerID := m.ContainerIDInputs[m.ActiveInputIndex].Value()
		partitionKey := m.PartitionKeyInputs[m.ActiveInputIndex].Value()

		if !strings.HasPrefix(partitionKey, "/") {
			partitionKey = "/" + partitionKey
		}

		return m, createContainer(m.NewDatabaseID, containerID, partitionKey)
	} else if m.ActiveInputIndex < m.ContainerCount-1 && m.CurrentView == CreateContainerView {
		m.ActiveInputIndex++
		containerID := m.ContainerIDInputs[m.ActiveInputIndex].Value()
		partitionKey := m.PartitionKeyInputs[m.ActiveInputIndex].Value()

		if !strings.HasPrefix(partitionKey, "/") {
			partitionKey = "/" + partitionKey
		}

		return m, createContainer(m.SelectedDatabase, containerID, partitionKey)
	}

	m.CurrentView = ContainerView
	return m, loadContainers(m.SelectedDatabase)
}

func (m *DatabaseModel) HandleDefaultMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.CurrentView == DatabaseView {
		m.Table, cmd = m.Table.Update(msg)
	} else if m.CurrentView == ContainerView {
		m.ContainerTable, cmd = m.ContainerTable.Update(msg)
	}

	return m, cmd
}

func (m *DatabaseModel) View() string {
	if m.Err != nil {
		log.Error("Error in database model", "error", m.Err)
		return fmt.Sprintf("Error: %v\nPress any key to exit.", m.Err)
	}

	if !m.Ready {
		return fmt.Sprintf("%s %s\n", m.Spinner.View(), m.LoadingText)
	}

	switch m.CurrentView {
	case DatabaseView:
		helpText := "\nUse arrow keys to navigate | Press r to refresh | c to create new database | q or ctrl+c to quit"
		if len(m.DbProperties) == 0 {
			return fmt.Sprintf("No databases found in this account.\n%s", helpText)
		}
		return util.SharedTableStyle.Render(m.Table.View()) + helpText

	case ContainerView:
		helpText := fmt.Sprintf("\nViewing containers in database: %s\nUse arrow keys to navigate | Press a to add container | esc to go back | q or ctrl+c to quit", m.SelectedDatabase)
		if len(m.Containers) == 0 {
			return fmt.Sprintf("No containers found in database %s.%s", m.SelectedDatabase, helpText)
		}
		return util.SharedTableStyle.Render(m.ContainerTable.View()) + helpText

	case CreateDatabaseView:
		s := "Create New Database\n\n"
		s += fmt.Sprintf("Database ID: %s\n", m.DatabaseIDInput.View())
		s += "\nIllegal characters: /, \\, ?, #\n"

		if m.DatabaseCreateError != "" {
			s += fmt.Sprintf("\nError: %s\n", m.DatabaseCreateError)
		}

		s += "\nControls:\n"
		s += "Enter: Continue to configure containers\n"
		s += "Esc: Cancel and go back\n"

		return s

	case ConfigureContainersView:
		s := fmt.Sprintf("Configure Containers for Database: %s\n\n", m.NewDatabaseID)
		s += "Add containers (up to 5):\n"
		s += "\nIllegal characters for container IDs: /, \\, ?, #\n"
		s += "Note: Dashes (-) are allowed in container IDs\n"

		for i := 0; i < m.ContainerCount; i++ {
			s += fmt.Sprintf("\nContainer %d ID: %s\n", i+1, m.ContainerIDInputs[i].View())
			s += fmt.Sprintf("Partition Key: %s\n", m.PartitionKeyInputs[i].View())
		}

		if m.DatabaseCreateError != "" {
			s += fmt.Sprintf("\nError: %s\n", m.DatabaseCreateError)
		}

		s += "\nControls:\n"
		s += "Tab: Switch between inputs\n"
		s += "Ctrl+A: Add another container (max 5)\n"
		s += "Ctrl+D: Remove a container (min 1)\n"
		s += "Enter: Create database with containers\n"
		s += "Esc: Go back to database ID input\n"

		return s

	case CreateContainerView:
		s := fmt.Sprintf("Create New Container(s) in %s\n\n", m.SelectedDatabase)
		s += "\nIllegal characters for container IDs: /, \\, ?, #\n"
		s += "Note: Dashes (-) are allowed in container IDs\n"

		for i := 0; i < m.ContainerCount; i++ {
			s += fmt.Sprintf("\nContainer %d ID: %s\n", i+1, m.ContainerIDInputs[i].View())
			s += fmt.Sprintf("Partition Key: %s\n", m.PartitionKeyInputs[i].View())
		}

		if m.ContainerCreateError != "" {
			s += fmt.Sprintf("\nError: %s\n", m.ContainerCreateError)
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

	s := &t
	util.ApplySharedTableStyle(s)
	return *s
}

func createContainerTable(containers []*ContainerInfo) table.Model {
	columns := []table.Column{
		{Title: "Container ID", Width: 30},
		{Title: "Partition Key", Width: 30},
		{Title: "Indexing Mode", Width: 15},
	}

	var rows []table.Row
	for _, container := range containers {
		rows = append(rows, table.Row{
			container.ID,
			container.PartitionKey,
			container.IndexingMode,
		})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := &t
	util.ApplySharedTableStyle(s)
	return *s
}

func AccountDetailsCmd(sp *services.ServiceProvider) *cobra.Command {
	var (
		accountName string
		verbose     bool
	)

	accountDetailsCmd := &cobra.Command{
		Use:                   "account-details",
		Short:                 "View and manage Cosmos DB account details",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			util.SetupLogging(verbose)

			account, err := GetAccountDetailsInternal(accountName, sp.DatabaseManager)
			if err != nil {
				log.Error(err.Error())
				return
			}

			runAccountDetailsUI(account)
		},
	}

	accountDetailsCmd.Flags().StringVarP(&accountName, "name", "n", "", "Account name to use (uses default if not specified)")
	accountDetailsCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output for debug logging")

	return accountDetailsCmd
}

func runAccountDetailsUI(account data.AccountOptions) {
	log.Debug("Using account", "name", account.Name)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	model := &DatabaseModel{
		Account:     account,
		CurrentView: DatabaseView,
		Spinner:     s,
		LoadingText: "Loading databases from account... Please wait.",
	}

	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		log.Fatal("Error running program", "error", err)
	}
}
