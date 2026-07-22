package dashboard

import (
	"strings"
	"testing"
)

// TestFrontendTooltipXSS covers B-16: tooltip construction must use DOM APIs
// (textContent, createElement, replaceChildren) and never inject user-supplied
// labels via innerHTML.
func TestFrontendTooltipXSS(t *testing.T) {
	resp, _ := Serve("/v0/resource/plugins/my-cpa-stats-plugin/app.js")
	src := string(resp.Body)

	// The tooltip functions (fillTooltip, clearTooltip) must not use innerHTML.
	// Extract the tooltip-related code section.
	fillIdx := strings.Index(src, "fillTooltip")
	clearIdx := strings.Index(src, "clearTooltip")
	if fillIdx < 0 || clearIdx < 0 {
		t.Fatal("missing tooltip functions")
	}
	// Check the section between clearTooltip and the next function definition.
	section := src[clearIdx:]
	if endIdx := strings.Index(section, "function placeTooltip"); endIdx > 0 {
		section = section[:endIdx]
	}
	if strings.Contains(section, "innerHTML") {
		t.Error("tooltip code must not use innerHTML")
	}
	if !strings.Contains(section, "textContent") {
		t.Error("tooltip must use textContent for labels")
	}
	if !strings.Contains(section, "replaceChildren") {
		t.Error("tooltip must use replaceChildren to clear previous content")
	}
}

// TestFrontendTooltipMultiSeries covers B-18: tooltip must list all subjects
// at the hovered time point, not just a single series.
func TestFrontendTooltipMultiSeries(t *testing.T) {
	resp, _ := Serve("/v0/resource/plugins/my-cpa-stats-plugin/app.js")
	src := string(resp.Body)

	if !strings.Contains(src, "rows.forEach") {
		t.Error("tooltip must iterate all rows (multi-series)")
	}
	if !strings.Contains(src, "compare-tooltip-row") {
		t.Error("tooltip must render per-row entries")
	}
}

// TestFrontendTooltipPageCoords covers B-18: tooltip positioning must use
// clientX/clientY page coordinates and body rect, not chart-local offsets.
func TestFrontendTooltipPageCoords(t *testing.T) {
	resp, _ := Serve("/v0/resource/plugins/my-cpa-stats-plugin/app.js")
	src := string(resp.Body)

	if !strings.Contains(src, "clientX") || !strings.Contains(src, "clientY") {
		t.Error("tooltip must use clientX/clientY for page coordinates")
	}
	if !strings.Contains(src, "getBoundingClientRect") {
		t.Error("tooltip must use getBoundingClientRect for body-relative positioning")
	}
}

// TestFrontendUPlotLoadOrder covers B-17: uPlot script must be loaded before
// app.js in the HTML.
func TestFrontendUPlotLoadOrder(t *testing.T) {
	resp, _ := Serve("/v0/resource/plugins/my-cpa-stats-plugin/index.html")
	src := string(resp.Body)

	uPlotIdx := strings.Index(src, "uPlot.iife.min.js")
	appIdx := strings.Index(src, "app.js")
	if uPlotIdx < 0 || appIdx < 0 {
		t.Fatal("missing script tags")
	}
	if uPlotIdx > appIdx {
		t.Error("uPlot must be loaded before app.js")
	}
}

// TestFrontendCSVZeroValues covers B-19: CSV export must use ?? (nullish
// coalescing) for point.at and point.value so legitimate zero values are
// preserved, not replaced with empty strings.
func TestFrontendCSVZeroValues(t *testing.T) {
	resp, _ := Serve("/v0/resource/plugins/my-cpa-stats-plugin/app.js")
	src := string(resp.Body)

	if !strings.Contains(src, "point.at ?? \"\"") {
		t.Error("CSV must use ?? for point.at to preserve zero values")
	}
	if !strings.Contains(src, "point.value ?? \"\"") {
		t.Error("CSV must use ?? for point.value to preserve zero values")
	}
}
