package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Entirely synthetic. Never captures production headers, IDs or quota bodies.
const four = `{"groups":[{"displayName":"Gemini","buckets":[{"window":"five_hour","remainingFraction":0.42,"resetTime":"2030-01-01T12:00:00Z"},{"window":"weekly","remainingFraction":0.31}]},{"displayName":"Claude / GPT","buckets":[{"window":"5h","remainingFraction":0.23},{"window":"week","remainingFraction":0.17}]}]}`

func TestSummary(t *testing.T) {
	b, _ := newBridge("http://127.0.0.1:1", "synthetic-admin", map[string]string{"Gemini": "gemini", "Claude / GPT": "claude-gpt"})
	groups, err := parseSummary([]byte(four), b.families)
	if err != nil || len(groups) != 4 || len(missing(groups)) != 0 {
		t.Fatal("four windows not parsed")
	}
	raw := `{"groups":[{"displayName":"Gemini","buckets":[{"window":"5h","remainingFraction":0.2},{"window":"mystery","remainingFraction":0.9}]}]}`
	g, err := parseSummary([]byte(raw), b.families)
	if err != nil || len(g) != 1 || len(missing(g)) != 3 {
		t.Fatal("missing windows invented")
	}
	for _, raw := range []string{`{}`, `{"groups":[{"displayName":"Unknown","buckets":[{"window":"5h","remainingFraction":0.5}]}]}`, `{"groups":[{"displayName":"Gemini","buckets":[{"window":"5h","remainingFraction":null}]}]}`} {
		if _, err := parseSummary([]byte(raw), b.families); err == nil {
			t.Fatal("accepted unknown/null schema")
		}
	}
	fallback := parseFallback([]byte(`{"models":{"claude-opus-test":{"quotaInfo":{"remainingFraction":0.6,"resetTime":"2030-01-01T00:00:00Z"}}}}`))
	if len(fallback) != 1 || fallback[0].Window != "" || fallback[0].Source != "fallback" {
		t.Fatal("fallback mislabeled")
	}
}
func TestSummaryDefaultGroupNames(t *testing.T) {
	t.Setenv("PI_USAGE_CPA_MANAGEMENT_ORIGIN", "http://127.0.0.1:1")
	t.Setenv("PI_USAGE_CPA_MANAGEMENT_KEY", "synthetic-admin")
	t.Setenv("PI_USAGE_CPA_GROUP_MAP", "")
	b, err := fromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic quota values with the exact observed upstream group names.
	raw := `{"groups":[{"displayName":"Gemini Models","buckets":[{"window":"weekly","remainingFraction":0.31},{"window":"5h","remainingFraction":0.42}]},{"displayName":"Claude and GPT models","buckets":[{"window":"weekly","remainingFraction":0.17},{"window":"5h","remainingFraction":0.23}]}]}`
	groups, err := parseSummary([]byte(raw), b.families)
	if err != nil || len(groups) != 4 || len(missing(groups)) != 0 {
		t.Fatalf("expected four explicit windows, got %+v, error %v", groups, err)
	}
	want := map[string]float64{"gemini-7d": 0.31, "gemini-5h": 0.42, "claude-gpt-7d": 0.17, "claude-gpt-5h": 0.23}
	for _, g := range groups {
		remaining, ok := want[g.ID]
		if !ok || g.ID != g.ModelGroup+"-"+g.Window || g.Remaining != remaining || g.Source != "summary" {
			t.Fatalf("incorrect family/window/quota: %+v", g)
		}
		delete(want, g.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing windows: %v", want)
	}
	for name, family := range map[string]string{"Gemini": "gemini", "Claude / GPT": "claude-gpt", "Claude/GPT": "claude-gpt"} {
		if b.families[name] != family {
			t.Fatalf("existing mapping changed: %q", name)
		}
	}
	for _, name := range []string{"Unknown", "Gemini Models preview", "claude and gpt models"} {
		unknown := strings.ReplaceAll(raw, "Gemini Models", name)
		unknown = strings.ReplaceAll(unknown, "Claude and GPT models", name)
		if groups, err := parseSummary([]byte(unknown), b.families); err == nil || len(groups) != 0 {
			t.Fatalf("unknown group %q must not be guessed: %+v", name, groups)
		}
	}
}

func TestCanceledRefreshDoesNotCachePartialAccounts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			fmt.Fprint(w, `{"files":[{"auth_index":"synthetic-claude","provider":"claude"}]}`)
			return
		}
		cancel()
		fmt.Fprint(w, `{"status_code":200,"body":"{\"five_hour\":{\"utilization\":20}}"}`)
	}))
	defer srv.Close()
	b, _ := newBridge(srv.URL, "synthetic-admin", nil)
	body, status := b.usage(ctx, false)
	if status != 503 || b.cached != nil || strings.Contains(string(body), "synthetic") {
		t.Fatalf("canceled refresh became a fresh partial cache: status=%d body=%s", status, body)
	}
}

func TestService(t *testing.T) {
	for _, upstreamStatus := range []int{200, 403, 429} {
		t.Run(fmt.Sprint(upstreamStatus), func(t *testing.T) {
			var mu sync.Mutex
			calls := 0
			fail := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer synthetic-admin" {
					t.Error("wrong server credential")
				}
				mu.Lock()
				defer mu.Unlock()
				if fail {
					w.WriteHeader(503)
					return
				}
				switch r.URL.Path {
				case "/v0/management/api-keys":
					fmt.Fprint(w, `{"api-keys":["synthetic-client"]}`)
				case "/v0/management/auth-files":
					fmt.Fprint(w, `{"files":[{"auth_index":"synthetic-private-a","provider":"antigravity","project_id":"synthetic-project-a"},{"auth_index":"synthetic-private-b","provider":"antigravity","project_id":"synthetic-project-b"}]}`)
				case "/v0/management/api-call":
					calls++
					var req struct {
						Index  string            `json:"authIndex"`
						URL    string            `json:"url"`
						Data   string            `json:"data"`
						Header map[string]string `json:"header"`
					}
					_ = json.NewDecoder(r.Body).Decode(&req)
					if req.Header["Authorization"] != "Bearer $TOKEN$" {
						t.Error("token placeholder missing")
					}
					var data map[string]string
					_ = json.Unmarshal([]byte(req.Data), &data)
					if (req.Index == "synthetic-private-a" && data["project"] != "synthetic-project-a") || (req.Index == "synthetic-private-b" && data["project"] != "synthetic-project-b") {
						t.Error("cross-account project")
					}
					body, status := four, upstreamStatus
					if req.URL == fallbackURL {
						body = `{"models":{"gemini-test":{"quotaInfo":{"remainingFraction":0.8}}}}`
						status = 200
					} else if req.URL != summaryURL {
						t.Error("arbitrary upstream")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"status_code": status, "body": body})
				default:
					t.Error("unexpected management route")
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			b, err := newBridge(server.URL, "synthetic-admin", map[string]string{"Gemini": "gemini", "Claude / GPT": "claude-gpt"})
			if err != nil {
				t.Fatal(err)
			}
			headers := http.Header{"Authorization": []string{"Bearer synthetic-client"}}
			if _, status := b.handle("GET", "/usage", http.Header{}, nil); status != 401 {
				t.Fatal("missing auth accepted")
			}
			if _, status := b.handle("POST", "/usage", headers, nil); status != 405 {
				t.Fatal("write accepted")
			}
			if _, status := b.handle("GET", "/usage", headers, url.Values{"url": []string{"https://example.invalid"}}); status != 400 {
				t.Fatal("proxy query accepted")
			}
			body, status := b.handle("GET", "/usage", headers, nil)
			if status != 200 {
				t.Fatal(status)
			}
			for _, secret := range []string{"synthetic-private", "synthetic-project", "synthetic-admin", "synthetic-client", "$TOKEN$"} {
				if strings.Contains(string(body), secret) {
					t.Fatal("secret disclosure")
				}
			}
			var doc struct {
				Schema   int       `json:"schemaVersion"`
				Accounts []account `json:"accounts"`
			}
			if json.Unmarshal(body, &doc) != nil || doc.Schema != 1 || len(doc.Accounts) != 2 || doc.Accounts[0].ID == doc.Accounts[1].ID {
				t.Fatal("account/legacy schema")
			}
			if upstreamStatus != 200 {
				for _, a := range doc.Accounts {
					if len(a.Groups) != 1 || a.Groups[0].Window != "" || len(a.Missing) != 4 || !strings.Contains(a.Error, fmt.Sprint(upstreamStatus)) {
						t.Fatal("failure fallback fabricated")
					}
				}
			}
			before := calls
			_, _ = b.handle("GET", "/usage", headers, nil)
			if calls != before {
				t.Fatal("cache miss")
			}
			if _, status := b.handle("GET", "/usage", headers, url.Values{"refresh": []string{"1"}}); status != 429 {
				t.Fatal("refresh unthrottled")
			}
			b.attempted = time.Now().Add(-refreshInterval - time.Second)
			if _, status := b.handle("GET", "/usage", headers, url.Values{"refresh": []string{"1"}}); status != 200 || calls == before {
				t.Fatal("refresh failed")
			}
			b.attempted = time.Now().Add(-ttl - time.Second)
			mu.Lock()
			fail = true
			mu.Unlock()
			body, status = b.usage(context.Background(), false)
			if status != 200 || !strings.Contains(string(body), `"stale":true`) {
				t.Fatal("stale cache unavailable")
			}
		})
	}
}
func TestSecurity(t *testing.T) {
	for _, origin := range []string{"https://example.invalid", "http://localhost:8317", "http://127.0.0.1:8317/other", "http://user@127.0.0.1:8317"} {
		if _, err := newBridge(origin, "synthetic", nil); err == nil {
			t.Fatal("non-loopback origin accepted")
		}
	}
	b, _ := newBridge("http://127.0.0.1:1", "synthetic", nil)
	b.client.Timeout = 10 * time.Millisecond
	if b.authorized(context.Background(), http.Header{"Authorization": []string{"Bearer synthetic"}}) {
		t.Fatal("failed open")
	}
	if b.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("environment proxy enabled")
	}
	if b.client.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirect permitted")
	}
}
