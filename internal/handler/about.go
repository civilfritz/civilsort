package handler

import (
	"log"
	"net/http"
)

// HandleAbout renders the about page.
func (h *Handler) HandleAbout(w http.ResponseWriter, r *http.Request) {
	if err := h.templates.ExecuteTemplate(w, "about.html", nil); err != nil {
		log.Printf("Error rendering about: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
