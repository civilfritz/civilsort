package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleAbout(t *testing.T) {
	h := testHandler(t)
	for _, target := range []string{"/about", "/about?from=abc123"} {
		rec := httptest.NewRecorder()
		h.HandleAbout(rec, httptest.NewRequest("GET", target, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected status 200, got %d", target, rec.Code)
		}
	}
}
