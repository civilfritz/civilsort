package main

import (
	"embed"
	"flag"
	"html/template"
	"io/fs"
	"log"
	"net/http"

	"github.com/civilfritz/civilsort/internal/db"
	"github.com/civilfritz/civilsort/internal/handler"
	"github.com/civilfritz/civilsort/internal/hub"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "civilsort.db", "SQLite database path")
	flag.Parse()

	// Open database
	database, err := db.Open(*dbPath)
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
	log.Printf("Listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, h.UserMiddleware(mux)))
}
