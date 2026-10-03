package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const summaryURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
const fallbackURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
const ttl = 120 * time.Second
const refreshInterval = 30 * time.Second

// Management traffic stays on loopback. No management URL, credential selector,
// upstream URL or headers are accepted from downstream requests.
type bridge struct {
	origin, key        string
	client             *http.Client
	families           map[string]string
	salt               [32]byte
	mu                 sync.Mutex
	cached             []byte
	updated, attempted time.Time
}
type group struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	ModelGroup string  `json:"modelGroup"`
	Window     string  `json:"window,omitempty"`
	Remaining  float64 `json:"remainingFraction"`
	Reset      string  `json:"resetTime,omitempty"`
	Source     string  `json:"source"`
}
type account struct {
	Provider    string   `json:"provider"`
	ID          string   `json:"authIndex"` // opaque plugin-local pseudonym, never CPA authIndex
	Label       string   `json:"label"`
	Groups      []group  `json:"groups"`
	Missing     []string `json:"missingWindows,omitempty"`
	Error       string   `json:"error,omitempty"`
	Disabled    bool     `json:"disabled,omitempty"`
	Unavailable bool     `json:"unavailable,omitempty"`
}
type authFile struct {
	Name         string                     `json:"name"`
	RuntimeOnly  bool                       `json:"runtime_only"`
	Domain       string                     `json:"domain"`
	Index        string                     `json:"auth_index"`
	Provider     string                     `json:"provider"`
	Type         string                     `json:"type"`
	Project      string                     `json:"project_id"`
	ProjectCamel string                     `json:"projectId"`
	Metadata     map[string]json.RawMessage `json:"metadata"`
	Attributes   map[string]json.RawMessage `json:"attributes"`
	Disabled     bool                       `json:"disabled"`
	Unavailable  bool                       `json:"unavailable"`
}

func newBridge(origin, key string, families map[string]string) (*bridge, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "[::1]" && u.Hostname() != "::1") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || key == "" {
		return nil, errors.New("loopback management origin and server key required")
	}
	for _, family := range families {
		if family != "gemini" && family != "claude-gpt" {
			return nil, errors.New("invalid model group mapping")
		}
	}
	b := &bridge{origin: strings.TrimRight(origin, "/"), key: key, families: families, client: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if _, err := rand.Read(b.salt[:]); err != nil {
		return nil, errors.New("pseudonym initialization failed")
	}
	return b, nil
}
func fromEnvironment() (*bridge, error) {
	origin := os.Getenv("PI_USAGE_CPA_MANAGEMENT_ORIGIN")
	if origin == "" {
		origin = "http://127.0.0.1:8317"
	}
	families := map[string]string{
		"Gemini":                "gemini",
		"Gemini Models":         "gemini",
		"Claude / GPT":          "claude-gpt",
		"Claude/GPT":            "claude-gpt",
		"Claude and GPT models": "claude-gpt",
	}
	if raw := os.Getenv("PI_USAGE_CPA_GROUP_MAP"); raw != "" {
		if json.Unmarshal([]byte(raw), &families) != nil {
			return nil, errors.New("invalid group mapping JSON")
		}
	}
	key := os.Getenv("PI_USAGE_CPA_MANAGEMENT_KEY")
	if key == "" {
		// CPA v8 can already hold the plaintext management password in its process
		// environment. Reuse it without writing a second copy to plugin config.
		key = os.Getenv("MANAGEMENT_PASSWORD")
	}
	return newBridge(origin, key, families)
}
func (b *bridge) management(ctx context.Context, path string, payload any, out any) error {
	var body io.Reader
	method := http.MethodGet
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return errors.New("invalid request")
		}
		body = strings.NewReader(string(raw))
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, b.origin+"/v0/management/"+path, body)
	if err != nil {
		return errors.New("management request failed")
	}
	req.Header.Set("Authorization", "Bearer "+b.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return errors.New("management unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("management HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil || len(raw) > 4*1024*1024 || json.Unmarshal(raw, out) != nil {
		return errors.New("invalid management response")
	}
	return nil
}
func (b *bridge) authorized(ctx context.Context, header http.Header) bool {
	value := header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return false
	}
	key := strings.TrimPrefix(value, "Bearer ")
	if key == "" || len(key) > 4096 {
		return false
	}
	var result struct {
		Keys []string `json:"api-keys"`
	}
	if b.management(ctx, "api-keys", nil, &result) != nil {
		return false
	}
	supplied := sha256.Sum256([]byte(key))
	match := 0
	for _, k := range result.Keys {
		if k == "" {
			continue
		}
		expected := sha256.Sum256([]byte(k))
		match |= subtle.ConstantTimeCompare(supplied[:], expected[:])
	}
	return match == 1
}
func (b *bridge) upstream(ctx context.Context, file authFile, endpoint string) ([]byte, int, error) {
	data, _ := json.Marshal(map[string]string{"project": file.Project})
	payload := map[string]any{"authIndex": file.Index, "method": "POST", "url": endpoint, "header": map[string]string{"Authorization": "Bearer $TOKEN$", "Content-Type": "application/json", "User-Agent": "antigravity/cli/1.20.0 (pi-usage-cpa; os_type=linux; arch=x64)"}, "data": string(data)}
	var result struct {
		Status int    `json:"status_code"`
		Body   string `json:"body"`
	}
	if err := b.management(ctx, "api-call", payload, &result); err != nil {
		return nil, 0, err
	}
	if result.Status != 200 {
		return nil, result.Status, fmt.Errorf("quota HTTP %d", result.Status)
	}
	return []byte(result.Body), result.Status, nil
}
func text(m map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var s string
		if json.Unmarshal(m[key], &s) == nil && s != "" {
			return s
		}
	}
	return ""
}
func fraction(m map[string]json.RawMessage) (float64, bool) {
	for _, key := range []string{"remainingFraction", "remaining_fraction"} {
		raw := m[key]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var n float64
		if json.Unmarshal(raw, &n) == nil && n >= 0 && n <= 1 {
			return n, true
		}
	}
	return 0, false
}
func reset(m map[string]json.RawMessage) string {
	s := text(m, "resetTime", "reset_time")
	if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
		return ""
	}
	return s
}
func normalizeWindow(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "5h", "five-hour", "five_hour":
		return "5h"
	case "weekly", "week", "7d":
		return "7d"
	}
	return ""
}

// Unknown labels are not guessed from fractions, reset times or bucket display names.
func parseSummary(raw []byte, families map[string]string) ([]group, error) {
	var payload struct {
		Groups []struct {
			Display string                       `json:"displayName"`
			Snake   string                       `json:"display_name"`
			Buckets []map[string]json.RawMessage `json:"buckets"`
		} `json:"groups"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Groups == nil {
		return nil, errors.New("summary schema unavailable")
	}
	groups := []group{}
	seen := map[string]bool{}
	for _, g := range payload.Groups {
		name := g.Display
		if name == "" {
			name = g.Snake
		}
		family := families[name]
		if family == "" {
			continue
		}
		for _, bucket := range g.Buckets {
			w := normalizeWindow(text(bucket, "window"))
			n, ok := fraction(bucket)
			if w == "" || !ok {
				continue
			}
			id := family + "-" + w
			if seen[id] {
				return nil, errors.New("ambiguous summary windows")
			}
			seen[id] = true
			groups = append(groups, group{ID: id, Label: w, ModelGroup: family, Window: w, Remaining: n, Reset: reset(bucket), Source: "summary"})
		}
	}
	if len(groups) == 0 {
		return nil, errors.New("summary windows unavailable")
	}
	return groups, nil
}
func modelFamily(id string) string {
	s := strings.ToLower(id)
	if strings.HasPrefix(s, "gemini-") {
		return "gemini"
	}
	if strings.HasPrefix(s, "claude-") || strings.HasPrefix(s, "gpt-") {
		return "claude-gpt"
	}
	return ""
}
func parseFallback(raw []byte) []group {
	var payload struct {
		Models map[string]struct {
			Quota map[string]json.RawMessage `json:"quotaInfo"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	// A model query does not establish a shared pool: preserve one record per model.
	groups := []group{}
	for id, m := range payload.Models {
		f := modelFamily(id)
		n, ok := fraction(m.Quota)
		if f == "" || !ok {
			continue
		}
		groups = append(groups, group{ID: id, Label: "Quota (window unknown)", ModelGroup: f, Remaining: n, Reset: reset(m.Quota), Source: "fallback"})
	}
	return groups
}
func missing(groups []group) []string {
	result := []string{}
	for _, f := range []string{"gemini", "claude-gpt"} {
		for _, w := range []string{"5h", "7d"} {
			found := false
			for _, g := range groups {
				if g.ModelGroup == f && g.Window == w {
					found = true
				}
			}
			if !found {
				result = append(result, f+"-"+w)
			}
		}
	}
	return result
}
func (b *bridge) pseudonym(index string) string {
	h := hmac.New(sha256.New, b.salt[:])
	h.Write([]byte(index))
	return hex.EncodeToString(h.Sum(nil)[:12])
}
func (b *bridge) load(ctx context.Context) ([]account, error) {
	var result struct {
		Files []authFile `json:"files"`
	}
	if err := b.management(ctx, "auth-files", nil, &result); err != nil {
		return nil, err
	}
	accounts := []account{}
	for _, file := range result.Files {
		provider := supportedProvider(file)
		if provider == "" {
			continue
		}
		a := account{Provider: provider, ID: b.pseudonym(file.Index), Label: fmt.Sprintf("%s %d", provider, len(accounts)+1), Groups: []group{}, Disabled: file.Disabled, Unavailable: file.Unavailable}
		if file.Project == "" {
			file.Project = file.ProjectCamel
		}
		if file.Project == "" {
			file.Project = text(file.Metadata, "project_id", "projectId")
		}
		if file.Project == "" {
			file.Project = text(file.Attributes, "project_id", "projectId", "gemini_virtual_project")
		}
		if !file.Disabled && !file.Unavailable && provider != "antigravity" {
			if file.Index == "" {
				a.Error = "credential selector missing"
			} else {
				var err error
				a.Groups, err = b.providerQuota(ctx, file, provider)
				if err != nil {
					a.Error = "quota unavailable"
				}
			}
		} else if !file.Disabled && !file.Unavailable {
			if file.Index == "" || file.Project == "" {
				a.Error = "credential project or selector missing"
			} else {
				raw, status, err := b.upstream(ctx, file, summaryURL)
				if err == nil {
					a.Groups, err = parseSummary(raw, b.families)
				}
				if err != nil {
					a.Error = "summary unavailable"
					if status != 0 && status != 200 {
						a.Error = fmt.Sprintf("summary HTTP %d", status)
					}
					fallback, _, fallbackErr := b.upstream(ctx, file, fallbackURL)
					if fallbackErr == nil {
						a.Groups = parseFallback(fallback)
					}
				}
			}
		}
		if a.Groups == nil {
			a.Groups = []group{}
		}
		if provider == "antigravity" {
			a.Missing = missing(a.Groups)
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}
func (b *bridge) usage(ctx context.Context, force bool) ([]byte, int) {
	// ponytail: one lock coalesces refreshes; per-account concurrency only if large pools need it.
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if force && !b.attempted.IsZero() && now.Sub(b.attempted) < refreshInterval {
		return []byte(`{"error":"refresh rate limited"}`), 429
	}
	if !force && !b.attempted.IsZero() && now.Sub(b.attempted) < ttl {
		if b.cached != nil {
			return append([]byte(nil), b.cached...), 200
		}
		return []byte(`{"error":"quota service unavailable"}`), 503
	}
	b.attempted = now
	accounts, err := b.load(ctx)
	if err != nil {
		if b.cached != nil {
			var data map[string]any
			_ = json.Unmarshal(b.cached, &data)
			data["cache"] = map[string]any{"updatedAt": b.updated.UTC().Format(time.RFC3339), "ttlMs": ttl.Milliseconds(), "stale": true}
			raw, _ := json.Marshal(data)
			b.cached = raw
			return raw, 200
		}
		return []byte(`{"error":"quota service unavailable"}`), 503
	}
	raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "generatedAt": now.UTC().Format(time.RFC3339), "cache": map[string]any{"updatedAt": now.UTC().Format(time.RFC3339), "ttlMs": ttl.Milliseconds(), "stale": false}, "accounts": accounts})
	b.cached = raw
	b.updated = now
	return append([]byte(nil), raw...), 200
}
func (b *bridge) handle(method, path string, headers http.Header, query url.Values) ([]byte, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if method != "GET" {
		return []byte(`{"error":"read only"}`), 405
	}
	if !b.authorized(ctx, headers) {
		return []byte(`{"error":"unauthorized"}`), 401
	}
	for k := range query {
		if k != "refresh" {
			return []byte(`{"error":"unsupported query"}`), 400
		}
	}
	switch path {
	case "/usage":
		return b.usage(ctx, query.Get("refresh") == "1")
	case "/capabilities", "/well-known":
		return []byte(`{"schemaVersion":1,"pluginId":"pi-usage-cpa","usage":"/v0/resource/plugins/pi-usage-cpa/usage","modelGroups":["gemini","claude-gpt","claude","codex","kimi","xai","devin","meta"],"windows":["5h","7d","daily","monthly","unknown"],"routingAccount":"unknown"}`), 200
	default:
		return []byte(`{"error":"not found"}`), 404
	}
}
