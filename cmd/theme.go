package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/colbytimm/alchemist/internal/theme"
)

func newThemeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theme",
		Short: "List, choose and print color themes",
		Long: "A theme colors the whole TUI. The built-in themes ship with Alchemist; a custom\n" +
			"theme is a .toml file in the themes folder beside config.toml. ctrl+t in the\n" +
			"app chooses a theme too, and saves it the way theme use does.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newThemeListCmd(), newThemeUseCmd(), newThemeShowCmd())
	return cmd
}

func newThemeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every theme, marking the saved one with *",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			themes, err := defaultThemes()
			if err != nil {
				return err
			}
			return writeThemeList(cmd.OutOrStdout(), themes.List(), themes.Saved())
		},
	}
}

func writeThemeList(w io.Writer, entries []theme.Entry, saved string) error {
	var rows strings.Builder
	for _, entry := range entries {
		mark := " "
		if entry.Name == saved {
			mark = "*"
		}
		fmt.Fprintf(&rows, "%s %s\t%s\t%s\n", mark, entry.Name, themeCredit(entry), themeWhere(entry))
	}
	if err := writeTable(w, rows.String()); err != nil {
		return fmt.Errorf("cmd: write themes: %w", err)
	}
	return nil
}

func themeCredit(entry theme.Entry) string {
	switch {
	case entry.Ignored:
		return "ignored: " + entry.Err.Error()
	case entry.Err != nil:
		return "cannot load: " + theme.Problem(entry.Err)
	case entry.About.Author == "":
		return entry.About.Title
	}
	return entry.About.Title + " by " + entry.About.Author
}

func themeWhere(entry theme.Entry) string {
	if entry.BuiltIn {
		return "built-in"
	}
	return entry.Path
}

func newThemeUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Save the theme every launch opens in",
		Long: "use checks the theme loads, then saves it as theme in config.toml, creating the\n" +
			"file if there is none. Profiles and every other setting are kept. A theme that\n" +
			"does not load is refused and config.toml is left as it was.",
		Example: "  alchemist theme use dracula-at-midnight\n" +
			"  alchemist theme use alchemist   # back to the default",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			themes, err := defaultThemes()
			if err != nil {
				return err
			}
			if err := themes.Save(args[0]); err != nil {
				return err
			}
			return say(cmd, "theme %s saved: alchemist opens in it from now on", args[0])
		},
	}
}

func newThemeShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show <name>",
		Short:   "Print a theme's file, to copy and edit",
		Example: "  alchemist theme show alchemist > ~/.config/alchemist/themes/mine.toml",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := theme.File(args[0], customThemes())
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
}
