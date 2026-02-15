package handler

import (
	"database/sql"
	"html/template"
	"net/http"

	"github.com/civilfritz/civilsort/internal/hub"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	db        *sql.DB
	templates *template.Template
	hub       *hub.Manager
}

// New creates a new Handler with the given dependencies.
func New(db *sql.DB, templates *template.Template, hubManager *hub.Manager) *Handler {
	return &Handler{
		db:        db,
		templates: templates,
		hub:       hubManager,
	}
}

// RegisterRoutes registers all HTTP routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.HandleHome)
	mux.HandleFunc("POST /", h.HandleCreateBallot)
	mux.HandleFunc("GET /ballot/{id}", h.HandleBallot)
	mux.HandleFunc("GET /ballot/{id}/{participantID}", h.HandleBallot)
	mux.HandleFunc("POST /ballot/{id}/items", h.HandleAddItem)
	mux.HandleFunc("POST /ballot/{id}/items/{itemID}/delete", h.HandleDeleteItem)
	mux.HandleFunc("POST /ballot/{id}/rankings", h.HandleSaveRankings)
	mux.HandleFunc("POST /ballot/{id}/toggle", h.HandleToggleOpen)
	mux.HandleFunc("POST /ballot/{id}/name", h.HandleSetName)
	mux.HandleFunc("GET /ballot/{id}/rankings/{participantID}", h.HandleGetRankings)
	mux.HandleFunc("GET /ballot/{id}/ws", h.HandleWebSocket)
	mux.HandleFunc("GET /about", h.HandleAbout)
	mux.HandleFunc("GET /stats", h.HandleStats)
}
