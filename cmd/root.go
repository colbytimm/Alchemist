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

	// Local storage management commands
	localCmd := &cobra.Command{
		Use:   "local",
		Short: "Manage local storage for accounts and queries",
		Long:  "Commands for managing local storage of Cosmos DB accounts and saved queries",
	}

	localCmd.AddCommand(LocalAccountAddCmd(sp))
	localCmd.AddCommand(LocalAccountDeleteCmd(sp))
	localCmd.AddCommand(LocalAccountListCmd(sp))
	localCmd.AddCommand(LocalQuerySaveCmd(sp))
	localCmd.AddCommand(LocalQueryListCmd(sp))
	localCmd.AddCommand(LocalQueryDeleteCmd(sp))

	// Cosmos DB operations commands
	cosmosCmd := &cobra.Command{
		Use:   "cosmos",
		Short: "Interact with Cosmos DB",
		Long:  "Commands for interacting directly with Cosmos DB resources and data",
	}

	cosmosCmd.AddCommand(CosmosAccountManageCmd(sp))
	cosmosCmd.AddCommand(CosmosDataQueryCmd(sp))
	cosmosCmd.AddCommand(CosmosDataUploadCmd(sp))

	rootCmd.AddCommand(localCmd)
	rootCmd.AddCommand(cosmosCmd)

	return rootCmd
}
