package main

import (
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/civilfritz/civilsort/internal/cli"
	"github.com/civilfritz/civilsort/internal/db"
	"github.com/civilfritz/civilsort/internal/handler"
	"github.com/civilfritz/civilsort/internal/hub"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

func main() {
	// Check for CLI subcommands before flag parsing
	if len(os.Args) > 1 && os.Args[1] == "list" {
		// CLI mode: create separate flag set
		fs := flag.NewFlagSet("list", flag.ExitOnError)
		dbDefault := "civilsort.db"
		if v := os.Getenv("CIVILSORT_DB"); v != "" {
			dbDefault = v
		}
		dbPath := fs.String("db", dbDefault, "SQLite database path (env: CIVILSORT_DB)")
		fs.Parse(os.Args[2:])

		// Open database
		database, err := db.Open(*dbPath)
		if err != nil {
			log.Fatalf("Failed to open database: %v", err)
		}
		defer database.Close()

		// Run CLI command
		if err := cli.Run(database, fs.Args()); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Server mode: original code
	var addr, dbPath string

	addrDefault := ":8080"
	if v := os.Getenv("CIVILSORT_ADDR"); v != "" {
		addrDefault = v
	}
	flag.StringVar(&addr, "addr", addrDefault, "listen address (env: CIVILSORT_ADDR)")

	dbDefault := "civilsort.db"
	if v := os.Getenv("CIVILSORT_DB"); v != "" {
		dbDefault = v
	}
	flag.StringVar(&dbPath, "db", dbDefault, "SQLite database path (env: CIVILSORT_DB)")
	flag.Parse()

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
}
