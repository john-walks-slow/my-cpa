// Package dashboard serves the embedded stats dashboard UI and computes
// server-side insight KPIs. It shares the stats plugin's aggregator read-only.
package dashboard

import "embed"

//go:embed all:web/dist
var webFS embed.FS
