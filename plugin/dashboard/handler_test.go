package dashboard

import (
	"strings"
	"testing"
)

func TestServeIndexHTML(t *testing.T) {
	resp, handled := Serve("/v0/resource/plugins/my-cpa-stats-plugin/index.html")
	if !handled {
		t.Fatal("expected handled")
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Headers.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
	if resp.Headers.Get("Cache-Control") != "no-cache" {
		t.Errorf("cache-control = %q, want no-cache", resp.Headers.Get("Cache-Control"))
	}
	csp := resp.Headers.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP missing script-src 'self': %q", csp)
	}
	if !strings.Contains(string(resp.Body), "<!DOCTYPE html>") {
		t.Error("body does not look like HTML")
	}
}

func TestServeAssets(t *testing.T) {
	cases := []struct {
		path string
		mime string
	}{
		{"/v0/resource/plugins/my-cpa-stats-plugin/app.js", "javascript"},
		{"/v0/resource/plugins/my-cpa-stats-plugin/app.css", "text/css"},
		{"/v0/resource/plugins/my-cpa-stats-plugin/uPlot.iife.min.js", "javascript"},
		{"/v0/resource/plugins/my-cpa-stats-plugin/uPlot.min.css", "text/css"},
	}
	for _, tc := range cases {
		resp, handled := Serve(tc.path)
		if !handled {
			t.Fatalf("%s: expected handled", tc.path)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status = %d, want 200", tc.path, resp.StatusCode)
		}
		ct := resp.Headers.Get("Content-Type")
		if !strings.Contains(ct, tc.mime) {
			t.Errorf("%s: content-type = %q, want contains %q", tc.path, ct, tc.mime)
		}
		if resp.Headers.Get("Cache-Control") != "public, max-age=3600" {
			t.Errorf("%s: cache-control = %q", tc.path, resp.Headers.Get("Cache-Control"))
		}
		if len(resp.Body) == 0 {
			t.Errorf("%s: empty body", tc.path)
		}
	}
}

func TestServeUnknownPath(t *testing.T) {
	paths := []string{
		"/v0/resource/plugins/my-cpa-stats-plugin/../../etc/passwd",
		"/v0/resource/plugins/my-cpa-stats-plugin/secret.txt",
		"/v0/resource/plugins/my-cpa-stats-plugin/assets/foo.js",
		"/v0/resource/plugins/my-cpa-stats-plugin/",
		"/v0/resource/plugins/my-cpa-stats-plugin",
	}
	for _, p := range paths {
		resp, handled := Serve(p)
		if !handled {
			t.Fatalf("%s: expected handled (404)", p)
		}
		if resp.StatusCode != 404 {
			t.Errorf("%s: status = %d, want 404", p, resp.StatusCode)
		}
	}
}

func TestServeUnrelatedPath(t *testing.T) {
	_, handled := Serve("/v0/management/stats/overview")
	if handled {
		t.Error("expected not handled for non-resource path")
	}
}

func TestResourcePaths(t *testing.T) {
	paths := ResourcePaths()
	if len(paths) != 6 {
		t.Fatalf("ResourcePaths() = %d paths, want 6", len(paths))
	}
	found := map[string]bool{}
	for _, p := range paths {
		found[p] = true
	}
	for _, want := range []string{"/index.html", "/app.js", "/app.css", "/uPlot.iife.min.js", "/uPlot.min.css", "/share.html"} {
		if !found[want] {
			t.Errorf("missing path %q", want)
		}
	}
	if found["/share-data"] {
		t.Error("/share-data must not be a static asset (it is served by publicShare handler)")
	}
}

func TestSecurityHeaders(t *testing.T) {
	resp, _ := Serve("/v0/resource/plugins/my-cpa-stats-plugin/app.js")
	if resp.Headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff")
	}
	if resp.Headers.Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options: DENY")
	}
}
