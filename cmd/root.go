package cmd

import (
	"github.com/colbytimm/alchemist/app"
	"github.com/colbytimm/alchemist/services"
	"github.com/logrusorgru/aurora/v4"
	"github.com/spf13/cobra"
)

func Root(sp *services.ServiceProvider) *cobra.Command {
	logo := `
 ______     __         ______     __  __     ______     __    __     __     ______     ______
/\  __ \   /\ \       /\  ___\   /\ \_\ \   /\  ___\   /\ "-./  \   /\ \   /\  ___\   /\__  _\
\ \  __ \  \ \ \____  \ \ \____  \ \  __ \  \ \  __\   \ \ \-./\ \  \ \ \  \ \___  \  \/_/\ \/
 \ \_\ \_\  \ \_____\  \ \_____\  \ \_\ \_\  \ \_____\  \ \_\ \ \_\  \ \_\  \/\_____\    \ \_\
  \/_/\/_/   \/_____/   \/_____/   \/_/\/_/   \/_____/   \/_/  \/_/   \/_/   \/_____/     \/_/
	`

	rootCmd := &cobra.Command{
		Use:     "alchemist",
		Long:    aurora.Magenta(logo).String() + "\n" + aurora.Blue("A command line tool for querying Cosmos DB in your terminal").String(),
		Version: app.Version,
	}

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddCommand(AddAccountCmd(sp))
	rootCmd.AddCommand(DeleteAccountCmd(sp))
	rootCmd.AddCommand(ListAccountCmd(sp))
	rootCmd.AddCommand(QueryAccountCmd(sp))
	rootCmd.AddCommand(AccountDetailsCmd(sp))
	rootCmd.AddCommand(BatchUploadCmd(sp))
	rootCmd.AddCommand(SaveQueryCmd(sp))
	rootCmd.AddCommand(ListQueryCmd(sp))
	rootCmd.AddCommand(DeleteQueryCmd(sp))

	return rootCmd
}
