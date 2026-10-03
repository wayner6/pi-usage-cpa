package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRegistration(t *testing.T) {
	t.Setenv("PI_USAGE_CPA_MANAGEMENT_ORIGIN", "http://127.0.0.1:8317")
	t.Setenv("PI_USAGE_CPA_MANAGEMENT_KEY", "synthetic-admin")
	t.Setenv("MANAGEMENT_PASSWORD", "synthetic-host-admin")
	t.Setenv("PI_USAGE_CPA_GROUP_MAP", `{"Gemini":"gemini","Claude / GPT":"claude-gpt"}`)
	for _, method := range []string{"plugin.register", "plugin.reconfigure"} {
		var e struct {
			OK     bool `json:"ok"`
			Result struct {
				Schema       int `json:"schema_version"`
				Metadata     struct{ Name, Version string }
				Capabilities map[string]bool
			}
		}
		if json.Unmarshal(dispatch(method, nil), &e) != nil || !e.OK || e.Result.Schema != 1 || e.Result.Metadata.Name != "pi-usage-cpa" || e.Result.Metadata.Version != pluginVersion || !e.Result.Capabilities["management_api"] {
			t.Fatal("invalid ABI registration")
		}
	}
	var e struct {
		OK     bool
		Result struct{ Resources []struct{ Path string } }
	}
	if json.Unmarshal(dispatch("management.register", nil), &e) != nil || !e.OK || len(e.Result.Resources) != 3 {
		t.Fatal("route registration")
	}
	for _, r := range e.Result.Resources {
		if strings.Contains(r.Path, "pi-bridge") {
			t.Fatal("legacy route collision")
		}
	}
	if !strings.Contains(string(dispatch("host.auth.save", nil)), `"ok":false`) {
		t.Fatal("unexpected write operation")
	}
	if b, err := fromEnvironment(); err != nil || b.key != "synthetic-admin" {
		t.Fatal("plugin-specific key should take priority")
	}
	t.Setenv("PI_USAGE_CPA_MANAGEMENT_KEY", "")
	if b, err := fromEnvironment(); err != nil || b.key != "synthetic-host-admin" {
		t.Fatal("existing v8 management password not reused")
	}
	t.Setenv("MANAGEMENT_PASSWORD", "")
	if !strings.Contains(string(dispatch("plugin.reconfigure", nil)), `"ok":false`) {
		t.Fatal("missing secret accepted")
	}
}
