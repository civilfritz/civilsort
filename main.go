package main

import (
	"embed"
	"flag"
	"html/template"
	"log"
	"net/http"

	"github.com/civilfritz/voting/internal/db"
	"github.com/civilfritz/voting/internal/handler"
	"github.com/civilfritz/voting/internal/hub"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "voting.db", "SQLite database path")
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
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	// Start server
	log.Printf("Listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, h.UserMiddleware(mux)))
}
