package handler

import (
	"log"
	"net/http"
)

// HandleAbout renders the about page.
func (h *Handler) HandleAbout(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"FromBallot": r.URL.Query().Get("from"),
	}
	if err := h.templates.ExecuteTemplate(w, "about.html", data); err != nil {
		log.Printf("Error rendering about: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
