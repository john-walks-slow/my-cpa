package main

import (
	"encoding/json"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type registrationCapability struct {
	UsagePlugin   bool `json:"usage_plugin"`
	ManagementAPI bool `json:"management_api"`
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             "my-cpa-stats-plugin",
			Version:          "0.1.0",
			Author:           "John",
			GitHubRepository: "https://github.com/John/my-cpa",
			ConfigFields: []pluginapi.ConfigField{
				{Name: "enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Enable the stats aggregator."},
				{Name: "retention_minutes", Type: pluginapi.ConfigFieldTypeInteger, Description: "Retention window in minutes (default 1440)."},
				{Name: "persist_path", Type: pluginapi.ConfigFieldTypeString, Description: "Snapshot file path. Empty means no persistence."},
				{Name: "persist_interval_sec", Type: pluginapi.ConfigFieldTypeInteger, Description: "Snapshot interval seconds (default 30)."},
				{Name: "cardinality_limit", Type: pluginapi.ConfigFieldTypeInteger, Description: "Max unique series kept (default 50000)."},
				{Name: "dashboard_enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Enable the embedded stats dashboard (default true)."},
				{Name: "share_enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Enable immutable public comparison snapshots."},
				{Name: "share_path", Type: pluginapi.ConfigFieldTypeString, Description: "Directory for share snapshots; defaults beside persist_path."},
				{Name: "share_max_count", Type: pluginapi.ConfigFieldTypeInteger, Description: "Maximum retained share snapshots; zero means unlimited."},
				{Name: "share_cleanup_interval_sec", Type: pluginapi.ConfigFieldTypeInteger, Description: "Share cleanup interval in seconds."},
				{Name: "share_max_snapshot_bytes", Type: pluginapi.ConfigFieldTypeInteger, Description: "Maximum serialized share snapshot size."},
			},
		},
		Capabilities: registrationCapability{
			UsagePlugin:   true,
			ManagementAPI: true,
		},
	}
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

func (p *pluginState) handleRegister(raw []byte) ([]byte, error) {
	var req lifecycleRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
	}
	if err := p.configure(req.ConfigYAML); err != nil {
		return nil, err
	}
	return okEnvelope(pluginRegistration())
}
