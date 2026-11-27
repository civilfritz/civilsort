package handler

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/civilfritz/civilsort/internal/util"
)

// participantCookieFrom returns the participant cookie set by a response.
func participantCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == participantCookie {
			return c
		}
	}
	t.Fatalf("response set no %s cookie", participantCookie)
	return nil
}

func TestSetParticipantCookie(t *testing.T) {
	tests := map[string]struct {
		configure  func(*http.Request)
		wantSecure bool
	}{
		"plain http":   {func(*http.Request) {}, false},
		"behind proxy": {func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") }, true},
		"direct tls":   {func(r *http.Request) { r.TLS = &tls.ConnectionState{} }, true},
		"proxy, http":  {func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "http") }, false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			tt.configure(r)
			rec := httptest.NewRecorder()
			setParticipantCookie(rec, r, "abc", "s3cret")

			c := participantCookieFrom(t, rec)
			if c.Secure != tt.wantSecure {
				t.Errorf("Secure = %v, want %v", c.Secure, tt.wantSecure)
			}
			if c.Path != "/ballot/abc" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("unexpected cookie attributes: %+v", c)
			}
		})
	}
}

// A participant's cookie identifies them only on their own ballot.
func TestCookieIsPerBallot(t *testing.T) {
	h := testHandler(t)
	ballotA, creatorA := newBallot(t, h)
	ballotB, _ := newBallot(t, h)
	setOpen(t, h, ballotB, false)

	rec := do(t, h.HandleToggleOpen, "POST", req{as: &creatorA, path: map[string]string{"id": ballotB}})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("ballot A's creator toggling ballot B: got %d, want 401", rec.Code)
	}
	if rec := do(t, h.HandleToggleOpen, "POST", req{as: &creatorA, path: map[string]string{"id": ballotA}}); rec.Code != http.StatusOK {
		t.Errorf("toggling own ballot: got %d, want 200", rec.Code)
	}
}

func TestHandleJoinLink(t *testing.T) {
	h := testHandler(t)
	ballotID, _ := newBallot(t, h)
	member := join(t, h, ballotID)

	for _, param := range []string{"secret", "legacySecret"} {
		t.Run(param, func(t *testing.T) {
			rec := do(t, h.HandleJoinLink, "GET", req{path: map[string]string{"id": ballotID, param: member.Secret}})

			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ballot/"+ballotID {
				t.Fatalf("got %d to %q, want 303 to /ballot/%s", rec.Code, rec.Header().Get("Location"), ballotID)
			}
			if c := participantCookieFrom(t, rec); c.Value != member.Secret {
				t.Errorf("cookie = %q, want the member's secret", c.Value)
			}
		})
	}

	rec := do(t, h.HandleJoinLink, "GET", req{path: map[string]string{"id": ballotID, "secret": "wrong"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown secret: got %d, want 404", rec.Code)
	}

	otherBallot, _ := newBallot(t, h)
	rec = do(t, h.HandleJoinLink, "GET", req{path: map[string]string{"id": otherBallot, "secret": member.Secret}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("secret used on another ballot: got %d, want 404", rec.Code)
	}
}

// Browsers from before per-ballot identity have only a voter_id cookie.
func TestLegacyVoterIDUpgrade(t *testing.T) {
	h := testHandler(t)
	ballotID, _ := newBallot(t, h)
	member := join(t, h, ballotID)
	voterID := util.GenerateUUID()
	if _, err := h.db.Exec("UPDATE participants SET legacy_user_id = ? WHERE id = ?", voterID, member.ID); err != nil {
		t.Fatal(err)
	}
	legacy := []*http.Cookie{{Name: legacyCookie, Value: voterID}}

	rec := do(t, h.HandleSetName, "POST", req{
		cookies: legacy,
		path:    map[string]string{"id": ballotID},
		form:    map[string][]string{"name": {"Upgraded"}},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("legacy cookie request: got %d, want 204", rec.Code)
	}
	if c := participantCookieFrom(t, rec); c.Value != member.Secret {
		t.Errorf("upgrade issued cookie %q, want the member's secret", c.Value)
	}

	otherBallot, _ := newBallot(t, h)
	rec = do(t, h.HandleSetName, "POST", req{
		cookies: legacy,
		path:    map[string]string{"id": otherBallot},
		form:    map[string][]string{"name": {"X"}},
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("legacy cookie on a ballot it never joined: got %d, want 401", rec.Code)
	}
}
