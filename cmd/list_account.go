package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/colbytimm/alchemist/data"
	"github.com/colbytimm/alchemist/services"
	"github.com/colbytimm/alchemist/util"
	"github.com/spf13/cobra"
)

func ListAccountInternal(dbManager data.DatabaseManager, verbose bool) ([]data.AccountOptions, error) {
	err := dbManager.OpenDatabase()
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}

	accounts, err := dbManager.GetAccounts()
	if err != nil {
		return nil, fmt.Errorf("could not retrieve accounts: %w", err)
	}

	return accounts, nil
}

func GetEnvironmentCellColour(environment string) string {
	switch strings.ToLower(environment) {
	case "dev":
		return ""
	case "stg":
		return ""
	case "prod", "prd":
		return ""
	}
	return ""
}

type deleteAction int

const (
	noAction deleteAction = iota
	confirmDelete
	cancelDelete
)

type model struct {
	table           table.Model
	accounts        []data.AccountOptions
	showConfirm     bool
	selectedAccount string
	deleteAction    deleteAction
	dbManager       data.DatabaseManager
}

type refreshAccountsMsg struct {
	accounts []data.AccountOptions
}

func (m *model) loadAccounts() tea.Cmd {
	return func() tea.Msg {
		accounts, err := ListAccountInternal(m.dbManager, false)
		if err != nil {
			return refreshAccountsMsg{accounts: []data.AccountOptions{}}
		}
		return refreshAccountsMsg{accounts: accounts}
	}
}

func (m *model) deleteAccount(name string) tea.Cmd {
	return func() tea.Msg {
		err := m.dbManager.OpenDatabase()
		if err == nil {
			err = m.dbManager.DeleteAccountByName(name)
			if err != nil {
				fmt.Printf("Error deleting account: %v\n", err)
			}
		}
		return m.loadAccounts()()
	}
}

func (m *model) Init() tea.Cmd {
	return m.loadAccounts()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshAccountsMsg:
		m.accounts = msg.accounts
		m.table = createAccountTable(m.accounts)
		return m, nil

	case tea.KeyMsg:
		if m.showConfirm {
			switch msg.String() {
			case "y", "Y":
				m.showConfirm = false
				m.deleteAction = confirmDelete
				cmd := m.deleteAccount(m.selectedAccount)
				return m, cmd
			case "n", "N", "esc":
				m.showConfirm = false
				m.deleteAction = cancelDelete
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "esc":
			if m.table.Focused() {
				m.table.Blur()
			} else {
				m.table.Focus()
			}
		case "q", "ctrl+c":
			return m, tea.Quit
		case "d", "enter":
			err := m.dbManager.OpenDatabase()
			if err != nil {
				fmt.Printf("Error opening database: %v\n", err)
				return m, tea.Quit
			}

			rowName := m.table.SelectedRow()[1]
			if rowName == "" {
				return m, nil
			}

			err = m.dbManager.UpdateDefaultItem(rowName)
			if err != nil {
				fmt.Printf("Error updating default account: %v\n", err)
				return m, tea.Quit
			}

			cmd := m.loadAccounts()
			return m, cmd

		case "x":
			rowName := m.table.SelectedRow()[1]
			if rowName == "" {
				return m, nil
			}

			m.selectedAccount = rowName
			m.showConfirm = true
			return m, nil
		}
	}

	var tableCmd tea.Cmd
	m.table, tableCmd = m.table.Update(msg)
	return m, tableCmd
}

func (m *model) View() string {
	if m.showConfirm {
		confirmStyle := lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 2).
			BorderTop(true).
			BorderLeft(true).
			BorderRight(true).
			BorderBottom(true)

		message := fmt.Sprintf("Are you sure you want to delete account '%s'?\n\n[y] Yes  [n] No", m.selectedAccount)
		return confirmStyle.Render(message)
	}

	tableView := util.SharedTableStyle.Render(m.table.View())
	helpText := "\nUse arrow keys to navigate | d or enter to set account as default | x to delete account | q or ctrl+c to quit"

	return tableView + helpText
}

func maskConnectionString(connectionString string) string {
	regex := regexp.MustCompile(`AccountKey=([^;]+);`)
	maskedString := regex.ReplaceAllString(connectionString, "AccountKey=***;")

	return maskedString
}

func isDefaultCheckmark(value bool) string {
	if value {
		return "\u2713"
	}
	return ""
}

func LocalAccountListCmd(sp *services.ServiceProvider) *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:                   "account list",
		Short:                 "List and manage Cosmos DB accounts from local storage",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			util.SetupLogging(verbose)

			err := sp.DatabaseManager.OpenDatabase()
			if err != nil {
				log.Error("Could not open database", "error", err)
				log.Info("Hint: Make sure you've added at least one account using 'alchemist local account add'")
				return
			}

			err = sp.DatabaseManager.EnsureAccountTableExists()
			if err != nil {
				log.Error("Could not ensure account table exists", "error", err)
				log.Info("Hint: Make sure you've added at least one account using 'alchemist local account add'")
				return
			}

			accounts, err := sp.DatabaseManager.GetAccounts()
			if err != nil {
				log.Error("Could not retrieve accounts", "error", err)
				return
			}

			if len(accounts) == 0 {
				log.Info("No accounts found. Add an account using 'alchemist local account add'")
				return
			}

			initialTable := createAccountTable(accounts)
			initialModel := &model{
				table:     initialTable,
				accounts:  accounts,
				dbManager: sp.DatabaseManager,
			}

			p := tea.NewProgram(initialModel)
			if _, err := p.Run(); err != nil {
				log.Fatal("Error running program", "error", err)
			}
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output for debug logging")

	return cmd
}

func createAccountTable(accounts []data.AccountOptions) table.Model {
	columns := []table.Column{
		{Title: "Default", Width: 8},
		{Title: "Name", Width: 20},
		{Title: "Tag", Width: 4},
		{Title: "Connection String", Width: 100},
	}

	var rows []table.Row
	for _, account := range accounts {
		rows = append(rows, table.Row{
			isDefaultCheckmark(account.IsDefault),
			account.Name,
			account.Tag,
			maskConnectionString(account.ConnectionString),
		})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(7),
	)

	return t
}
