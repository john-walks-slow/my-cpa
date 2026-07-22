package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/John/my-cpa/plugin/aggregator"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func (p *pluginState) handleUsage(raw []byte) ([]byte, error) {
	p.mu.Lock()
	enabled := p.cfg.Enabled
	agg := p.agg
	p.mu.Unlock()

	if !enabled || agg == nil {
		return okEnvelope(struct{}{})
	}

	var rec pluginapi.UsageRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, err
	}

	agg.Ingest(aggregator.Sample{
		Provider:        rec.Provider,
		Model:           rec.Model,
		Alias:           rec.Alias,
		AuthID:          identityKey(rec),
		Source:          rec.Source,
		RequestedAt:     rec.RequestedAt,
		Latency:         rec.Latency,
		TTFT:            rec.TTFT,
		Failed:          rec.Failed,
		StatusCode:      rec.Failure.StatusCode,
		InputTokens:     rec.Detail.InputTokens,
		OutputTokens:    rec.Detail.OutputTokens,
		ReasoningTokens: rec.Detail.ReasoningTokens,
		CachedTokens:    rec.Detail.CachedTokens,
	})

	return okEnvelope(struct{}{})
}

func identityKey(rec pluginapi.UsageRecord) string {
	if rec.AuthID != "" {
		return rec.AuthID
	}
	if rec.APIKey != "" {
		digest := sha256.Sum256([]byte(rec.APIKey))
		return "api-key:" + hex.EncodeToString(digest[:8])
	}
	if rec.AuthIndex != "" {
		return rec.AuthType + ":" + rec.AuthIndex
	}
	return rec.Source + ":" + rec.Provider
}
