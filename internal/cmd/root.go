package cmd

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/civilfritz/civilsort/internal/db"
	"github.com/civilfritz/civilsort/internal/handler"
	"github.com/civilfritz/civilsort/internal/hub"
	"github.com/spf13/cobra"
)

var (
	dbPath string
	addr   string

	templateFS embed.FS
	staticFS   embed.FS
)

func NewRootCmd(tmpls embed.FS, static embed.FS) *cobra.Command {
	templateFS = tmpls
	staticFS = static

	rootCmd := &cobra.Command{
		Use:   "civilsort",
		Short: "Ranked-choice voting server using the Schulze method",
		RunE:  runServer,
	}

	// Persistent flag (shared by all commands)
	dbDefault := "civilsort.db"
	if v := os.Getenv("CIVILSORT_DB"); v != "" {
		dbDefault = v
	}
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", dbDefault, "SQLite database path (env: CIVILSORT_DB)")

	// Server-only flag
	addrDefault := ":8080"
	if v := os.Getenv("CIVILSORT_ADDR"); v != "" {
		addrDefault = v
	}
	rootCmd.Flags().StringVar(&addr, "addr", addrDefault, "listen address (env: CIVILSORT_ADDR)")

	// Add subcommands
	rootCmd.AddCommand(NewListCmd())

	return rootCmd
}

func runServer(cmd *cobra.Command, args []string) error {
	// Open database
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Parse templates
	tmpls, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		log.Fatalf("Failed to parse templates: %v", err)
	}

	// Create hub manager
	hubManager := hub.NewManager()
	defer hubManager.CloseAll()

	// Create handler
	h := handler.New(database, tmpls, hubManager)

	// Set up routes
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Serve static files from the static/ subdirectory in the embedded FS
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("Failed to create static sub-filesystem: %v", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	// Start server
	log.Printf("Listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, h.UserMiddleware(mux)))
	return nil
}

func Execute(tmpls embed.FS, static embed.FS) error {
	return NewRootCmd(tmpls, static).Execute()
}
