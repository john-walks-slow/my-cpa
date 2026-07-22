package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/John/my-cpa/plugin/share"
)

func TestShareCookieSecure(t *testing.T) {
	cases := []struct {
		name    string
		headers http.Header
		want    string
	}{
		{"plain HTTP", http.Header{}, "Path=/v0/resource/plugins/my-cpa-stats-plugin; HttpOnly; SameSite=Lax"},
		{"x-forwarded-proto https", http.Header{"X-Forwarded-Proto": []string{"https"}}, "Secure"},
		{"x-forwarded-proto HTTPS", http.Header{"X-Forwarded-Proto": []string{"HTTPS"}}, "Secure"},
		{"x-forwarded-ssl on", http.Header{"X-Forwarded-Ssl": []string{"on"}}, "Secure"},
		{"x-forwarded-proto http", http.Header{"X-Forwarded-Proto": []string{"http"}}, ""},
	}
	for _, tc := range cases {
		cookie := shareCookie("abc", tc.headers)
		if !strings.Contains(cookie, tc.want) {
			t.Errorf("%s: cookie = %q, want to contain %q", tc.name, cookie, tc.want)
		}
		if tc.want == "" && strings.Contains(cookie, "Secure") {
			t.Errorf("%s: cookie should not be Secure, got %q", tc.name, cookie)
		}
		if !strings.Contains(cookie, "Path=/v0/resource/plugins/my-cpa-stats-plugin") {
			t.Errorf("%s: missing Path scope: %q", tc.name, cookie)
		}
		if !strings.Contains(cookie, "HttpOnly") {
			t.Errorf("%s: missing HttpOnly: %q", tc.name, cookie)
		}
		if !strings.Contains(cookie, "SameSite=Lax") {
			t.Errorf("%s: missing SameSite=Lax: %q", tc.name, cookie)
		}
	}
}

func TestIsTLSRequest(t *testing.T) {
	cases := []struct {
		name    string
		headers http.Header
		want    bool
	}{
		{"empty", http.Header{}, false},
		{"x-forwarded-proto https", http.Header{"X-Forwarded-Proto": []string{"https"}}, true},
		{"x-forwarded-proto http", http.Header{"X-Forwarded-Proto": []string{"http"}}, false},
		{"x-forwarded-ssl on", http.Header{"X-Forwarded-Ssl": []string{"on"}}, true},
		{"front-end-https on", http.Header{"Front-End-Https": []string{"on"}}, true},
	}
	for _, tc := range cases {
		if got := isTLSRequest(tc.headers); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestPublicShareNotFound covers S-06: the public share-data endpoint must
// return 404 for missing IDs, wrong tokens, and expired snapshots, and the
// error body must be indistinguishable across all failure modes.
func TestPublicShareNotFound(t *testing.T) {
	root := t.TempDir()
	store := share.New(root, 0, 1024*1024)
	created := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store.SetNow(func() time.Time { return created })

	expires := created.Add(time.Hour)
	token, hash, _ := share.NewToken()
	_ = hash
	id, err := store.Create(share.Snapshot{
		CreatedAt:    created,
		ExpiresAt:    &expires,
		RequireToken: true,
		TokenSHA256:  share.HashToken(token),
		Rows:         []string{"data"},
	})
	if err != nil {
		t.Fatal(err)
	}

	p := &pluginState{shares: store}

	cases := []struct {
		name  string
		query url.Values
	}{
		{"missing id", url.Values{}},
		{"nonexistent id", url.Values{"id": {"ZZZZZZZZZZ"}}},
		{"wrong token", url.Values{"id": {id}, "token": {"wrong"}}},
		{"no token for protected", url.Values{"id": {id}}},
	}
	for _, tc := range cases {
		resp := p.publicShare(tc.query, http.Header{})
		if resp.StatusCode != 404 {
			t.Errorf("%s: status = %d, want 404", tc.name, resp.StatusCode)
		}
		var body map[string]string
		if err := json.Unmarshal(resp.Body, &body); err != nil {
			t.Fatalf("%s: unmarshal: %v", tc.name, err)
		}
		if body["error"] != "not found" {
			t.Errorf("%s: error = %q, want 'not found'", tc.name, body["error"])
		}
	}

	// Expired snapshot must also 404.
	store.SetNow(func() time.Time { return expires.Add(time.Minute) })
	resp := p.publicShare(url.Values{"id": {id}, "token": {token}}, http.Header{})
	if resp.StatusCode != 404 {
		t.Errorf("expired: status = %d, want 404", resp.StatusCode)
	}
}

// TestPublicShareTokenAndCookie covers S-06: a valid token in the query string
// must return 200 and set a cookie; a subsequent request using the cookie
// (without query token) must also succeed.
func TestPublicShareTokenAndCookie(t *testing.T) {
	root := t.TempDir()
	store := share.New(root, 0, 1024*1024)
	created := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store.SetNow(func() time.Time { return created })

	token, _, _ := share.NewToken()
	id, err := store.Create(share.Snapshot{
		CreatedAt:    created,
		RequireToken: true,
		TokenSHA256:  share.HashToken(token),
		Rows:         []string{"locked-data"},
	})
	if err != nil {
		t.Fatal(err)
	}

	p := &pluginState{shares: store}

	// Query token → 200 + Set-Cookie.
	resp := p.publicShare(url.Values{"id": {id}, "token": {token}}, http.Header{})
	if resp.StatusCode != 200 {
		t.Fatalf("query token: status = %d, want 200", resp.StatusCode)
	}
	setCookie := resp.Headers.Get("Set-Cookie")
	if !strings.Contains(setCookie, "cpa_share_token=") {
		t.Errorf("missing Set-Cookie: %q", setCookie)
	}
	if !strings.Contains(setCookie, "HttpOnly") {
		t.Errorf("cookie missing HttpOnly: %q", setCookie)
	}

	// Cookie-based access → 200 without query token.
	headers := http.Header{}
	headers.Set("Cookie", "cpa_share_token="+token)
	resp2 := p.publicShare(url.Values{"id": {id}}, headers)
	if resp2.StatusCode != 200 {
		t.Errorf("cookie token: status = %d, want 200", resp2.StatusCode)
	}

	// Public (no token required) snapshot → 200 without any token.
	pubID, err := store.Create(share.Snapshot{CreatedAt: created, Rows: []string{"public"}})
	if err != nil {
		t.Fatal(err)
	}
	resp3 := p.publicShare(url.Values{"id": {pubID}}, http.Header{})
	if resp3.StatusCode != 200 {
		t.Errorf("public snapshot: status = %d, want 200", resp3.StatusCode)
	}
}

// TestPublicShareNoStore covers S-06: when share storage is not configured,
// the endpoint must return 404 (not 500 or panic).
func TestPublicShareNoStore(t *testing.T) {
	p := &pluginState{}
	resp := p.publicShare(url.Values{"id": {"abcdefghij"}}, http.Header{})
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
