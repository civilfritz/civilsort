package cmd

import (
	"log"

	"github.com/civilfritz/civilsort/internal/cli"
	"github.com/civilfritz/civilsort/internal/db"
	"github.com/spf13/cobra"
)

func NewListCmd() *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List ballots",
		RunE:  runListBallots,
	}

	listCmd.AddCommand(NewListItemsCmd())
	listCmd.AddCommand(NewListParticipantsCmd())

	return listCmd
}

func runListBallots(cmd *cobra.Command, args []string) error {
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	return cli.ListBallots(database)
}
