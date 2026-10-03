//go:build cpa_host_integration

// Copied into the pinned CPA source tree by test-cpa-v8.sh.
package pluginhost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestPiUsageCPAHostRegistration(t *testing.T) {
	// No production secrets, management server or upstream requests are needed.
	t.Setenv("PI_USAGE_CPA_MANAGEMENT_KEY", "synthetic-admin")
	t.Setenv("MANAGEMENT_PASSWORD", "")
	t.Setenv("PI_USAGE_CPA_MANAGEMENT_ORIGIN", "http://127.0.0.1:1")
	t.Setenv("PI_USAGE_CPA_GROUP_MAP", "")
	dir := os.Getenv("PI_USAGE_CPA_TEST_PLUGIN_DIR")
	if dir == "" {
		t.Fatal("test plugin directory required")
	}
	enabled := true
	cfg := &config.Config{Plugins: config.PluginsConfig{
		Enabled: true, Dir: dir,
		Configs: map[string]config.PluginInstanceConfig{
			"pi-usage-cpa": {Enabled: &enabled},
		},
	}}
	h := New() // Real dlopen loader, C ABI, RPC adapter and validPlugin validator.
	t.Cleanup(h.ShutdownAll)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, phase := range []string{"register", "reconfigure"} {
		t.Run(phase, func(t *testing.T) {
			h.ApplyConfig(ctx, cfg)
			if !h.PluginLoaded("pi-usage-cpa") || !h.PluginRegistered("pi-usage-cpa") {
				t.Fatal("native plugin must be loaded and registered in actual CPA host")
			}
			records := h.activeRecords()
			if len(records) != 1 || records[0].plugin.Capabilities.ManagementAPI == nil {
				t.Fatal("management_api capability must be active")
			}
			meta := records[0].meta
			if meta.Name == "" || meta.Version == "" || meta.Author == "" || meta.GitHubRepository != "https://github.com/wayner6/pi-usage-cpa" {
				t.Fatalf("incomplete host metadata: %+v", meta)
			}
			h.RegisterManagementRoutes(ctx, nil)
			for _, path := range []string{"capabilities", "usage", "well-known"} {
				req := httptest.NewRequest(http.MethodGet, "/v0/resource/plugins/pi-usage-cpa/"+path, nil)
				resp := httptest.NewRecorder()
				if !h.ServeResourceHTTP(resp, req) {
					t.Fatalf("resource route %s not registered", path)
				}
				if resp.Code != http.StatusUnauthorized {
					t.Fatalf("unauthenticated %s status = %d, want 401", path, resp.Code)
				}
			}
		})
	}
}
