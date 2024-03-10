package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/colbytimm/alchemist/data"
	"github.com/spf13/cobra"
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

type model struct {
	table table.Model
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

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
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
			data.OpenDatabase()
			rowName := m.table.SelectedRow()[1]
			data.UpdateDefaultItem(rowName)
			accounts := data.GetAccounts()
			var rows []table.Row
			for _, account := range accounts {
				rows = append(rows, table.Row{
					isDefaultCheckmark(account.IsDefault),
					account.Name,
					account.Tag,
					maskConnectionString(account.ConnectionString),
				})
			}
			m.table.SetRows(rows)

			return m, nil
		}
	}
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) View() string {
	tableView := baseStyle.Render(m.table.View()) + "\n"

	helpSection := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Width(100).
		Render("Help:\n" +
			"  d or enter: Set account as default\n" +
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
	addAccountCmd := &cobra.Command{
		Use:                   "list-account",
		Short:                 "List Cosmos DB accounts",
		Args:                  cobra.ExactArgs(0),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			data.OpenDatabase()
			accounts := data.GetAccounts()
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

			m := model{t}
			if _, err := tea.NewProgram(m).Run(); err != nil {
				fmt.Println("Error running program:", err)
				os.Exit(1)
			}
		},
	}

	return addAccountCmd
}
