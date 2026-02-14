package cmd

import (
	"log"

	"github.com/civilfritz/civilsort/internal/cli"
	"github.com/civilfritz/civilsort/internal/db"
	"github.com/spf13/cobra"
)

func NewListParticipantsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "participants <ballot-id>",
		Short: "List participants for a ballot",
		Args:  cobra.ExactArgs(1),
		RunE:  runListParticipants,
	}
}

func runListParticipants(cmd *cobra.Command, args []string) error {
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	return cli.ListParticipants(database, args[0])
}
