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
	"github.com/spf13/cobra"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

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
}

type refreshAccountsMsg struct {
	accounts []data.AccountOptions
}

func loadAccounts() tea.Cmd {
	return func() tea.Msg {
		var accounts []data.AccountOptions
		err := data.OpenDatabase()
		if err == nil {
			accounts, err = data.GetAccounts()
			if err != nil {
				accounts = []data.AccountOptions{}
			}
		}
		return refreshAccountsMsg{accounts: accounts}
	}
}

func deleteAccount(name string) tea.Cmd {
	return func() tea.Msg {
		err := data.OpenDatabase()
		if err == nil {
			err = data.DeleteAccountByName(name)
			if err != nil {
				fmt.Printf("Error deleting account: %v\n", err)
			}
		}
		return loadAccounts()()
	}
}

func (m model) Init() tea.Cmd {
	return loadAccounts()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
				return m, deleteAccount(m.selectedAccount)
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
			err := data.OpenDatabase()
			if err != nil {
				fmt.Printf("Error opening database: %v\n", err)
				return m, tea.Quit
			}

			rowName := m.table.SelectedRow()[1]
			if rowName == "" {
				return m, nil
			}

			err = data.UpdateDefaultItem(rowName)
			if err != nil {
				fmt.Printf("Error updating default account: %v\n", err)
				return m, tea.Quit
			}

			return m, loadAccounts()

		case "x":
			rowName := m.table.SelectedRow()[1]
			if rowName == "" {
				return m, nil
			}

			m.selectedAccount = rowName
			m.showConfirm = true
			return m, nil

		case "r":
			return m, loadAccounts()
		}
	}

	var tableCmd tea.Cmd
	m.table, tableCmd = m.table.Update(msg)
	return m, tableCmd
}

func (m model) View() string {
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

	tableView := baseStyle.Render(m.table.View()) + "\n"

	helpSection := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Width(100).
		Render("Help:\n" +
			"  d or enter: Set account as default\n" +
			"  x: Delete account (with confirmation)\n" +
			"  r: Refresh account list\n" +
			"  q or ctrl+c: Quit\n")

	return tableView + helpSection
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

func ListAccountCmd() *cobra.Command {
	listAccountCmd := &cobra.Command{
		Use:                   "list-account",
		Short:                 "List and manage Cosmos DB accounts",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			err := data.OpenDatabase()
			if err != nil {
				log.Error("Could not open database", "error", err)
				log.Info("Hint: Make sure you've added at least one account using 'alchemist add-account'")
				return
			}

			err = data.EnsureAccountTableExists()
			if err != nil {
				log.Error("Could not ensure account table exists", "error", err)
				log.Info("Hint: Make sure you've added at least one account using 'alchemist add-account'")
				return
			}

			accounts, err := data.GetAccounts()
			if err != nil {
				log.Error("Could not retrieve accounts", "error", err)
				return
			}

			if len(accounts) == 0 {
				log.Info("No accounts found. Add an account using 'alchemist add-account'")
				return
			}

			initialTable := createAccountTable(accounts)
			initialModel := model{
				table:    initialTable,
				accounts: accounts,
			}

			p := tea.NewProgram(initialModel)
			if _, err := p.Run(); err != nil {
				log.Fatal("Error running program", "error", err)
			}
		},
	}

	return listAccountCmd
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

	s := table.DefaultStyles()
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return t
}
