package dashboard

import (
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const resourceBasePath = "/v0/resource/plugins/my-cpa-stats-plugin"

var assetRoutes = map[string]string{
	"/index.html":        "web/dist/index.html",
	"/app.js":            "web/dist/app.js",
	"/app.css":           "web/dist/app.css",
	"/uPlot.iife.min.js": "web/dist/uPlot.iife.min.js",
	"/uPlot.min.css":     "web/dist/uPlot.min.css",
	"/share.html":        "web/dist/index.html",
}

// ResourcePaths returns the sub-paths to register as ResourceRoutes.
func ResourcePaths() []string {
	paths := make([]string, 0, len(assetRoutes))
	for p := range assetRoutes {
		paths = append(paths, p)
	}
	return paths
}

// Serve handles a resource request by exact sub-path match.
// Returns (response, true) if the path was handled.
func Serve(reqPath string) (pluginapi.ManagementResponse, bool) {
	sub, ok := strings.CutPrefix(reqPath, resourceBasePath)
	if !ok {
		return pluginapi.ManagementResponse{}, false
	}
	embedPath, known := assetRoutes[sub]
	if !known {
		return notFound(), true
	}
	return serveFile(embedPath)
}

func serveFile(name string) (pluginapi.ManagementResponse, bool) {
	data, err := fs.ReadFile(webFS, name)
	if err != nil {
		return notFound(), true
	}
	h := http.Header{}
	h.Set("Content-Type", mimeByExt(name))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")

	if strings.HasSuffix(name, ".html") {
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'none'")
	} else {
		h.Set("Cache-Control", "public, max-age=3600")
	}

	return pluginapi.ManagementResponse{StatusCode: 200, Headers: h, Body: data}, true
}

func notFound() pluginapi.ManagementResponse {
	h := http.Header{}
	h.Set("Content-Type", "text/plain; charset=utf-8")
	return pluginapi.ManagementResponse{
		StatusCode: 404,
		Headers:    h,
		Body:       []byte("not found"),
	}
}

func mimeByExt(name string) string {
	ext := filepath.Ext(name)
	m := mime.TypeByExtension(ext)
	if m == "" {
		switch ext {
		case ".js":
			return "text/javascript; charset=utf-8"
		case ".css":
			return "text/css; charset=utf-8"
		case ".html":
			return "text/html; charset=utf-8"
		}
		return "application/octet-stream"
	}
	return m
}
