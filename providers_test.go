package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderParsers(t *testing.T) {
	cases := []struct {
		name, raw string
		parse     func([]byte) []group
		count     int
		windows   []string
	}{
		{"claude", `{"five_hour":{"utilization":16,"resets_at":"2030-01-01T05:00:00Z"},"seven_day":{"utilization":39},"seven_day_opus":{"utilization":29}}`, parseClaude, 3, []string{"5h", "7d", "7d"}},
		{"codex", `{"rate_limit":{"primary_window":{"used_percent":19,"limit_window_seconds":18000},"secondary_window":{"used_percent":33,"limit_window_seconds":604800}},"code_review_rate_limit":{"primary_window":{"used_percent":3}}}`, parseCodex, 3, []string{"5h", "7d", ""}},
		{"kimi", `{"limits":[{"window":{"duration":5,"timeUnit":"hour"},"detail":{"used":2,"limit":10}}],"usages":{"limit_month_total":{"used_ratio":0.3}}}`, parseKimi, 2, []string{"5h", "monthly"}},
		{"xai", `{"config":{"creditUsagePercent":40,"currentPeriod":{"type":"weekly"},"monthlyLimit":{"val":100},"used":20}}`, parseXai, 2, []string{"7d", "monthly"}},
		{"devin", `{"userStatus":{"planStatus":{"dailyQuotaRemainingPercent":84,"weeklyQuotaRemainingPercent":61,"dailyQuotaResetAtUnix":1893456000}}}`, parseDevin, 2, []string{"daily", "7d"}},
		{"meta", `{"subs_usage":{"window":{"used_percent":16,"window_duration_mins":300},"weekly":{"used_percent":39}}}`, parseMeta, 2, []string{"5h", "7d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := tc.parse([]byte(tc.raw))
			if len(gs) != tc.count {
				t.Fatalf("groups: %v", gs)
			}
			for i, g := range gs {
				if g.ModelGroup != tc.name || g.Window != tc.windows[i] || g.Remaining < 0 || g.Remaining > 1 || g.Source != "summary" {
					t.Fatalf("invalid group %+v", g)
				}
			}
		})
	}
	for _, p := range []func([]byte) []group{parseClaude, parseCodex, parseKimi, parseXai, parseDevin, parseMeta} {
		if len(p([]byte(`{"used_percent":null,"remainingFraction":0.88}`))) != 0 {
			t.Fatal("invented quota")
		}
	}
	if got := parseCodex([]byte(`{"rate_limit":{"primary_window":{"used_percent":20}}}`)); len(got) != 1 || got[0].Window != "" {
		t.Fatal("guessed Codex period")
	}
	if got := parseMeta([]byte(`{"subs_usage":{"window":{"used_percent":2}}}`)); len(got) != 1 || got[0].Window != "" {
		t.Fatal("guessed Meta period")
	}
	if got := parseXai([]byte(`{"config":{"prepaidBalance":{"val":9999}}}`)); len(got) != 0 {
		t.Fatal("invented xAI quota from balance")
	}
}
func TestKimiTimeUnitEnums(t *testing.T) {
	cases := []struct {
		unit     string
		duration int
		window   string
	}{
		{"TIME_UNIT_MINUTE", 300, "5h"},
		{" time_unit_minutes ", 300, "5h"},
		{"TIME_UNIT_HOUR", 5, "5h"},
		{"TIME_UNIT_DAY", 7, "7d"},
		{"TIME_UNIT_DAYS", 1, "daily"},
		{"TIME_UNIT_WEEK", 1, "7d"},
		{"minute", 300, "5h"},
		{"TIME_UNIT_MINUTE", 60, ""},
		{"TIME_UNIT_UNSPECIFIED", 300, ""},
		{"", 300, ""},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%d", tc.unit, tc.duration), func(t *testing.T) {
			raw := fmt.Sprintf(`{"limits":[{"window":{"duration":%d,"timeUnit":%q},"detail":{"used":"2","limit":"10","resetTime":"2030-01-01T05:00:00Z"}}]}`, tc.duration, tc.unit)
			gs := parseKimi([]byte(raw))
			if len(gs) != 1 || gs[0].Window != tc.window || gs[0].Remaining != 0.8 {
				t.Fatalf("unexpected Kimi quota: %+v", gs)
			}
			if tc.window == "" && gs[0].Label != "Quota (window unknown)" {
				t.Fatalf("guessed window label: %+v", gs[0])
			}
		})
	}
}

func TestSevenAccountsAndCredentialIsolation(t *testing.T) {
	fixtures := map[string]string{
		"claude":      `{"five_hour":{"utilization":20}}`,
		"codex":       `{"rate_limit":{"primary_window":{"used_percent":30,"limit_window_seconds":18000}}}`,
		"kimi":        `{"limits":[{"window":{"duration":1,"timeUnit":"week"},"used":1,"limit":5}]}`,
		"xai":         `{"config":{"creditUsagePercent":25,"currentPeriod":{"type":"weekly"}}}`,
		"devin":       `{"userStatus":{"planStatus":{"dailyQuotaRemainingPercent":50}}}`,
		"meta":        `{"api_key":"synthetic-pii","subs_usage":{"weekly":{"used_percent":80}}}`,
		"antigravity": `{"groups":[{"displayName":"Gemini","buckets":[{"window":"5h","remainingFraction":0.9}]}]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-admin" {
			t.Error("admin boundary")
		}
		switch r.URL.Path {
		case "/v0/management/auth-files":
			files := []map[string]any{}
			for _, p := range []string{"antigravity", "claude", "codex", "kimi", "xai", "devin", "meta"} {
				f := map[string]any{"provider": p, "auth_index": "synthetic-" + p, "name": "synthetic-meta.json"}
				if p == "antigravity" {
					f["project_id"] = "synthetic-project"
				}
				files = append(files, f)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"files": files})
		case "/v0/management/auth-files/download":
			if r.URL.Query().Get("name") != "synthetic-meta.json" {
				t.Error("unexpected file")
			}
			fmt.Fprint(w, `{"dca_token":"dca:synthetic-secret","api_key":"synthetic-pii"}`)
		case "/v0/management/api-call":
			var req struct {
				Index  string            `json:"authIndex"`
				URL    string            `json:"url"`
				Method string            `json:"method"`
				Data   string            `json:"data"`
				Header map[string]string `json:"header"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			p := strings.TrimPrefix(req.Index, "synthetic-")
			if _, ok := fixtures[p]; !ok {
				t.Error("foreign account")
			}
			if p == "meta" {
				if req.Header["Authorization"] != "Bearer dca:synthetic-secret" {
					t.Error("meta DCA binding")
				}
			} else if p == "devin" {
				if !strings.Contains(req.Data, `"$TOKEN$"`) {
					t.Error("devin token placeholder")
				}
			} else if req.Header["Authorization"] != "Bearer $TOKEN$" {
				t.Error("account token placeholder")
			}
			if p == "antigravity" && !strings.Contains(req.Data, "synthetic-project") {
				t.Error("antigravity project missing")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "body": fixtures[p]})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	b, err := newBridge(srv.URL, "synthetic-admin", map[string]string{"Gemini": "gemini"})
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := b.load(context.Background())
	if err != nil || len(accounts) != 7 {
		t.Fatalf("accounts=%d err=%v", len(accounts), err)
	}
	for _, a := range accounts {
		if len(a.Groups) == 0 || a.Error != "" {
			t.Fatalf("%s not parsed: %+v", a.Provider, a)
		}
		raw, _ := json.Marshal(a)
		for _, secret := range []string{"synthetic-", "dca:", "api_key", "synthetic-pii", "project_id"} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("credential leaked")
			}
		}
	}
}
func TestKimiDomainFromOwnCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files/download" {
			fmt.Fprint(w, `{"domain":"ai","token":"synthetic-private"}`)
			return
		}
		if r.URL.Path != "/v0/management/api-call" {
			t.Error("unexpected route")
			w.WriteHeader(404)
			return
		}
		var req struct {
			Index string `json:"authIndex"`
			URL   string `json:"url"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.URL != "https://api.kimi.ai/coding/v1/usages" || req.Index != "synthetic-kimi-ai" {
			t.Error("Kimi credential/domain mismatch")
		}
		fmt.Fprint(w, `{"status_code":200,"body":"{\"limits\":[{\"used\":1,\"limit\":2}]}"}`)
	}))
	defer srv.Close()
	b, _ := newBridge(srv.URL, "synthetic-admin", nil)
	groups, err := b.providerQuota(context.Background(), authFile{Name: "synthetic.json", Index: "synthetic-kimi-ai", Provider: "kimi-coding"}, "kimi")
	if err != nil || len(groups) != 1 || groups[0].Window != "" {
		t.Fatal("unknown Kimi duration was guessed or domain lost")
	}
}

func TestXaiBillingFailureNeverTriggersChat(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			fmt.Fprint(w, `{"files":[{"auth_index":"synthetic-xai","provider":"xai"}]}`)
			return
		}
		if r.URL.Path != "/v0/management/api-call" {
			t.Error("unexpected management path")
			w.WriteHeader(404)
			return
		}
		calls++
		var req struct {
			URL    string `json:"url"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !strings.HasPrefix(req.URL, "https://cli-chat-proxy.grok.com/v1/billing") || req.Method != "GET" {
			t.Error("quota read issued chat or arbitrary request")
		}
		fmt.Fprint(w, `{"status_code":403,"body":"synthetic-private"}`)
	}))
	defer srv.Close()
	b, _ := newBridge(srv.URL, "synthetic-admin", nil)
	accounts, err := b.load(context.Background())
	if err != nil || len(accounts) != 1 || len(accounts[0].Groups) != 0 || accounts[0].Error != "quota unavailable" || calls != 2 {
		t.Fatal("paid health probe or invented quota")
	}
}

func TestProviderErrorsAreNotQuota(t *testing.T) {
	b, _ := newBridge("http://127.0.0.1:1", "synthetic-admin", nil)
	if p := supportedProvider(authFile{Provider: "unrelated"}); p != "" {
		t.Fatal("unknown provider accepted")
	}
	if _, err := b.metaDCA(context.Background(), authFile{Index: "synthetic", Name: "../../secrets.json"}); err == nil {
		t.Fatal("path traversal")
	}
	if len(parseClaude([]byte(`{"five_hour":{"utilization":101}}`))) != 0 {
		t.Fatal("invalid used percent")
	}
}
