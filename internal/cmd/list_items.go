package cmd

import (
	"log"

	"github.com/civilfritz/civilsort/internal/cli"
	"github.com/civilfritz/civilsort/internal/db"
	"github.com/spf13/cobra"
)

func NewListItemsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "items <ballot-id>",
		Short: "List items for a ballot",
		Args:  cobra.ExactArgs(1),
		RunE:  runListItems,
	}
}

func runListItems(cmd *cobra.Command, args []string) error {
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	return cli.ListItems(database, args[0])
}
