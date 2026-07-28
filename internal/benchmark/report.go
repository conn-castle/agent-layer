package benchmark

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"time"
)

//go:embed assets/report.html.tmpl
var reportAssets embed.FS

// Report is the canonical derived model shared by JSON and HTML rendering.
type Report struct {
	SchemaVersion    string                  `json:"schema_version"`
	ComparisonID     string                  `json:"comparison_id"`
	GeneratedAt      time.Time               `json:"generated_at"`
	ObservedCampaign *ObservedCampaignReport `json:"observed_campaign"`
	Limitations      []string                `json:"limitations"`
}

// RenderJSON renders canonical report JSON before the presentation document.
func RenderJSON(report Report) ([]byte, error) {
	if report.ObservedCampaign == nil {
		return nil, fmt.Errorf("benchmark report has no observed campaign")
	}
	return canonicalJSON(report)
}

// RenderHTML produces an offline document from already-derived report data.
func RenderHTML(report Report) ([]byte, error) {
	if report.ObservedCampaign == nil {
		return nil, fmt.Errorf("benchmark report has no observed campaign")
	}
	templateData, err := reportAssets.ReadFile("assets/report.html.tmpl")
	if err != nil {
		return nil, fmt.Errorf("read benchmark report template: %w", err)
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("encode benchmark report data: %w", err)
	}
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"json":   func() template.JS { return template.JS(bytes.ReplaceAll(reportJSON, []byte("</"), []byte("<\\/"))) },
		"mul100": func(value float64) float64 { return value * 100 },
		"add1":   func(value int) int { return value + 1 },
		"currentObservedVersion": func(campaign *ObservedCampaignReport) ObservedVersionReport {
			return campaign.Versions[len(campaign.Versions)-1]
		},
		"costRange": func(minimum, maximum float64) string {
			if math.Abs(maximum-minimum) < 0.0000005 {
				return fmt.Sprintf("$%.2f", minimum)
			}
			return fmt.Sprintf("$%.2f–$%.2f", minimum, maximum)
		},
	}).Parse(string(templateData)) // #nosec G203 -- report JSON is escaped before insertion into a non-executable data script.
	if err != nil {
		return nil, fmt.Errorf("parse benchmark report template: %w", err)
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, report); err != nil {
		return nil, fmt.Errorf("render benchmark report: %w", err)
	}
	return output.Bytes(), nil
}
