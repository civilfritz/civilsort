package handler

import (
	"net/http"
	"testing"
)

func TestHandleAbout(t *testing.T) {
	h := testHandler(t)
	req, rec, _ := authedRequest(t, h, "GET", "/about", nil)

	h.HandleAbout(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestHandleAbout_WithFromParam(t *testing.T) {
	h := testHandler(t)
	req, rec, _ := authedRequest(t, h, "GET", "/about?from=abc123", nil)

	h.HandleAbout(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}
