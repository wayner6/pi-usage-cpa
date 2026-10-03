package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func supportedProvider(file authFile) string {
	for _, raw := range []string{file.Provider, file.Type} {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "antigravity":
			return "antigravity"
		case "claude":
			return "claude"
		case "codex", "openai-codex":
			return "codex"
		case "kimi", "kimi-code", "kimi-coding", "kimi-ai":
			return "kimi"
		case "xai", "grok":
			return "xai"
		case "devin":
			return "devin"
		case "meta":
			return "meta"
		}
	}
	return ""
}

// Only fixed, read-only quota endpoints are allowed. Never accept URLs, headers,
// credentials, or auth indices from a downstream request.
func (b *bridge) quotaCall(ctx context.Context, file authFile, endpoint string, method string, headers map[string]string, data string) ([]byte, error) {
	payload := map[string]any{"authIndex": file.Index, "method": method, "url": endpoint, "header": headers}
	if data != "" {
		payload["data"] = data
	}
	var r struct {
		Status int    `json:"status_code"`
		Body   string `json:"body"`
	}
	if err := b.management(ctx, "api-call", payload, &r); err != nil {
		return nil, err
	}
	if r.Status != 200 {
		return nil, fmt.Errorf("quota HTTP %d", r.Status)
	}
	return []byte(r.Body), nil
}
func record(raw []byte) map[string]any { var m map[string]any; _ = json.Unmarshal(raw, &m); return m }
func obj(v any) map[string]any         { m, _ := v.(map[string]any); return m }
func array(v any) []any                { a, _ := v.([]any); return a }
func field(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) {
			return n, true
		}
	case json.Number:
		f, e := n.Float64()
		return f, e == nil
	case string:
		if strings.TrimSpace(n) == "" {
			return 0, false
		}
		f, e := strconv.ParseFloat(n, 64)
		return f, e == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return 0, false
}
func percent(v any) (float64, bool) { n, ok := number(v); return 1 - n/100, ok && n >= 0 && n <= 100 }
func quotaReset(v any) string {
	if s, ok := v.(string); ok {
		if d, e := time.Parse(time.RFC3339Nano, s); e == nil {
			return d.UTC().Format(time.RFC3339)
		}
	}
	if n, ok := number(v); ok && n >= 1e9 && n <= 1e11 {
		return time.Unix(int64(n), 0).UTC().Format(time.RFC3339)
	}
	return ""
}
func windowGroup(provider, id, label, window string, remaining float64, at string) group {
	return group{ID: id, Label: label, ModelGroup: provider, Window: window, Remaining: remaining, Reset: at, Source: "summary"}
}
func parseClaude(raw []byte) []group {
	root := record(raw)
	out := []group{}
	for _, entry := range []struct{ key, label, window string }{{"five_hour", "5h", "5h"}, {"seven_day", "7d", "7d"}, {"seven_day_opus", "Opus 7d", "7d"}, {"seven_day_sonnet", "Sonnet 7d", "7d"}, {"seven_day_oauth_apps", "OAuth apps 7d", "7d"}, {"seven_day_cowork", "Cowork 7d", "7d"}, {"iguana_necktie", "Fable 7d", "7d"}} {
		w := obj(root[entry.key])
		if n, ok := percent(w["utilization"]); ok {
			out = append(out, windowGroup("claude", entry.key, entry.label, entry.window, n, quotaReset(w["resets_at"])))
		}
	}
	return out
}
func codexWindow(w map[string]any, id string) (group, bool) {
	used, ok := percent(field(w, "used_percent", "usedPercent"))
	if !ok {
		return group{}, false
	}
	duration, hasDuration := number(field(w, "limit_window_seconds", "limitWindowSeconds"))
	label, window := "Quota (window unknown)", ""
	if hasDuration {
		switch {
		case duration == 18000:
			label, window = "5h", "5h"
		case duration == 604800:
			label, window = "7d", "7d"
		case duration >= 28*86400 && duration <= 31*86400:
			label, window = "Monthly", "monthly"
		}
	}
	reset := quotaReset(field(w, "reset_at", "resetAt"))
	if reset == "" {
		if seconds, ok := number(field(w, "reset_after_seconds", "resetAfterSeconds")); ok && seconds > 0 && seconds <= 365*86400 {
			reset = time.Now().Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
		}
	}
	return windowGroup("codex", id, label, window, used, reset), true
}
func parseCodex(raw []byte) []group {
	root := record(raw)
	out := []group{}
	for _, lim := range []struct{ key, prefix string }{{"rate_limit", "code"}, {"code_review_rate_limit", "review"}} {
		r := obj(root[lim.key])
		if lim.key == "rate_limit" && r == nil {
			r = obj(root["rateLimit"])
		}
		if lim.key == "code_review_rate_limit" && r == nil {
			r = obj(root["codeReviewRateLimit"])
		}
		for _, w := range []struct{ snake, camel string }{{"primary_window", "primaryWindow"}, {"secondary_window", "secondaryWindow"}} {
			if g, ok := codexWindow(obj(field(r, w.snake, w.camel)), lim.prefix+"-"+w.snake); ok {
				if lim.prefix == "review" {
					g.Label = "Review " + g.Label
				}
				out = append(out, g)
			}
		}
	}
	return out
}
func kimiPeriod(entry map[string]any) (string, string) {
	w := obj(entry["window"])
	d, ok := number(field(w, "duration"))
	unit, _ := field(w, "timeUnit").(string)
	if ok {
		switch strings.ToLower(unit) {
		case "hour", "hours":
			if d == 5 {
				return "5h", "5h"
			}
		case "day", "days":
			if d == 7 {
				return "7d", "7d"
			}
			if d == 1 {
				return "24h", "daily"
			}
		case "week", "weeks":
			if d == 1 {
				return "7d", "7d"
			}
		case "minute", "minutes":
			if d == 300 {
				return "5h", "5h"
			}
		}
	}
	// Scope names are not a reliable substitute for an explicit period.
	return "Quota (window unknown)", ""
}
func parseKimi(raw []byte) []group {
	root := record(raw)
	out := []group{}
	for i, item := range array(root["limits"]) {
		entry := obj(item)
		detail := obj(entry["detail"])
		if detail == nil {
			detail = entry
		}
		total, ok := number(detail["limit"])
		if !ok || total <= 0 {
			continue
		}
		used, hasUsed := number(detail["used"])
		if !hasUsed {
			if left, ok := number(detail["remaining"]); ok {
				used = total - left
				hasUsed = true
			}
		}
		if !hasUsed || used < 0 || used > total {
			continue
		}
		label, window := kimiPeriod(entry)
		out = append(out, windowGroup("kimi", fmt.Sprintf("limit-%d", i), label, window, 1-used/total, quotaReset(field(detail, "reset_at", "reset_time", "resetAt", "resetTime"))))
	}
	if monthly := obj(obj(root["usages"])["limit_month_total"]); monthly != nil {
		if used, ok := number(monthly["used_ratio"]); ok && used >= 0 && used <= 1 {
			out = append(out, windowGroup("kimi", "monthly", "Monthly", "monthly", 1-used, quotaReset(monthly["reset_time"])))
		}
	}
	if w := obj(root["usage"]); w != nil {
		total, ok := number(w["limit"])
		used, hasUsed := number(w["used"])
		if ok && hasUsed && total > 0 && used >= 0 && used <= total {
			out = append(out, windowGroup("kimi", "summary", "Quota (window unknown)", "", 1-used/total, quotaReset(field(w, "reset_at", "reset_time"))))
		}
	}
	return out
}
func parseXai(raw []byte) []group {
	c := obj(record(raw)["config"])
	if c == nil {
		return nil
	}
	out := []group{}
	if pct, ok := percent(field(c, "creditUsagePercent", "credit_usage_percent")); ok {
		p := obj(field(c, "currentPeriod", "current_period"))
		label, w := "Quota (window unknown)", ""
		if strings.EqualFold(fmt.Sprint(p["type"]), "weekly") {
			label, w = "7d", "7d"
		}
		out = append(out, windowGroup("xai", "billing-credits", label, w, pct, quotaReset(p["end"])))
	}
	// Do not turn a monetary balance into a model quota percentage. Included
	// monthly credits have an explicit limit and used amount; label as billing.
	lim := field(c, "monthlyLimit", "monthly_limit")
	if v := obj(lim); v != nil {
		lim = v["val"]
	}
	total, ok := number(lim)
	used, valid := number(c["used"])
	if ok && valid && total > 0 && used >= 0 && used <= total {
		out = append(out, windowGroup("xai", "monthly-billing", "Monthly included credits", "monthly", 1-used/total, quotaReset(field(c, "billingPeriodEnd", "billing_period_end"))))
	}
	return out
}
func parseDevin(raw []byte) []group {
	status := obj(obj(record(raw)["userStatus"])["planStatus"])
	out := []group{}
	for _, entry := range []struct{ key, label, window string }{{"daily", "Daily", "daily"}, {"weekly", "7d", "7d"}} {
		v, ok := number(status[entry.key+"QuotaRemainingPercent"])
		if !ok || v < 0 || v > 100 {
			continue
		}
		out = append(out, windowGroup("devin", entry.key, entry.label, entry.window, v/100, quotaReset(status[entry.key+"QuotaResetAtUnix"])))
	}
	return out
}
func parseMeta(raw []byte) []group {
	root := record(raw)
	u := obj(root["subs_usage"])
	out := []group{}
	for _, entry := range []struct{ key, label, window string }{{"window", "Quota (window unknown)", ""}, {"weekly", "7d", "7d"}} {
		w := obj(u[entry.key])
		remaining, ok := percent(w["used_percent"])
		if !ok {
			continue
		}
		label, window := entry.label, entry.window
		if entry.key == "window" {
			if minutes, ok := number(w["window_duration_mins"]); ok && minutes == 300 {
				label, window = "5h", "5h"
			}
		}
		out = append(out, windowGroup("meta", entry.key, label, window, remaining, quotaReset(w["resets_at"])))
	}
	return out
}

// Credential JSON stays inside a single refresh. Never include it in a response.
func (b *bridge) credentialData(ctx context.Context, file authFile) (map[string]any, error) {
	if file.Name == "" || file.RuntimeOnly || !strings.HasSuffix(strings.ToLower(file.Name), ".json") || strings.ContainsAny(file.Name, "/\\") {
		return nil, errors.New("credential file unavailable")
	}
	var m map[string]any
	if err := b.management(ctx, "auth-files/download?name="+url.QueryEscape(file.Name), nil, &m); err != nil {
		return nil, errors.New("credential file unavailable")
	}
	return m, nil
}

// Meta quota requires the account's persisted DCA token, not the LLM key.
func (b *bridge) metaDCA(ctx context.Context, file authFile) (string, error) {
	m, err := b.credentialData(ctx, file)
	if err != nil {
		return "", err
	}
	token, _ := m["dca_token"].(string)
	if !strings.HasPrefix(token, "dca:") || strings.ContainsAny(token, " \r\n\t") {
		return "", errors.New("meta credential unavailable")
	}
	return token, nil
}
func hasGroup(groups []group, id string) bool {
	for _, g := range groups {
		if g.ID == id {
			return true
		}
	}
	return false
}

func (b *bridge) providerQuota(ctx context.Context, file authFile, provider string) ([]group, error) {
	bearer := map[string]string{"Authorization": "Bearer $TOKEN$"}
	endpoint, method, headers, data := "", "GET", bearer, ""
	var parse func([]byte) []group
	switch provider {
	case "claude":
		endpoint = "https://api.anthropic.com/api/oauth/usage"
		headers = map[string]string{"Authorization": "Bearer $TOKEN$", "anthropic-beta": "oauth-2025-04-20", "User-Agent": "claude-cli/2.1.280 (external, cli)"}
		parse = parseClaude
	case "codex":
		endpoint = "https://chatgpt.com/backend-api/wham/usage"
		headers = map[string]string{"Authorization": "Bearer $TOKEN$", "User-Agent": "codex-tui/0.149.1"}
		parse = parseCodex
	case "kimi":
		endpoint = "https://api.kimi.com/coding/v1/usages"
		domain := file.Domain
		if domain == "" {
			domain = text(file.Metadata, "domain")
		}
		base := text(file.Metadata, "base_url", "base-url")
		if base == "" {
			base = text(file.Attributes, "base_url", "base-url")
		}
		if domain == "" && base == "" && file.Name != "" && !file.RuntimeOnly {
			if credential, err := b.credentialData(ctx, file); err == nil {
				if v, ok := credential["domain"].(string); ok {
					domain = v
				}
				if v, ok := credential["base_url"].(string); ok {
					base = v
				}
			}
		}
		u, _ := url.Parse(base)
		if domain == "ai" || domain == "kimi.ai" || strings.Contains(file.Provider, "kimi-ai") || u.Hostname() == "kimi.ai" || strings.HasSuffix(u.Hostname(), ".kimi.ai") {
			endpoint = "https://api.kimi.ai/coding/v1/usages"
		}
		parse = parseKimi
	case "xai":
		endpoint = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
		headers = map[string]string{"Authorization": "Bearer $TOKEN$", "x-xai-token-auth": "xai-grok-cli", "x-grok-client-version": "0.2.91", "User-Agent": "grok-pager/0.2.91 grok-shell/0.2.91 (macos; aarch64)"}
		parse = parseXai
	case "devin":
		endpoint = "https://server.codeium.com/exa.seat_management_pb.SeatManagementService/GetUserStatus"
		method = "POST"
		headers = map[string]string{"Content-Type": "application/json", "Connect-Protocol-Version": "1"}
		data = `{"metadata":{"ideName":"chisel","ideVersion":"3000.10.21","apiKey":"$TOKEN$","locale":"en","os":"darwin","extensionVersion":"3000.10.21","clientName":"chisel"}}`
		parse = parseDevin
	case "meta":
		endpoint = "https://api.meta.ai/muse-code/key"
		method = "POST"
		dca, err := b.metaDCA(ctx, file)
		if err != nil {
			return nil, err
		}
		headers = map[string]string{"Authorization": "Bearer " + dca, "Content-Type": "application/json", "Accept": "application/json", "x-api-version": "1.0.0"}
		data = "{}"
		parse = parseMeta
	default:
		return nil, errors.New("unsupported provider")
	}
	raw, err := b.quotaCall(ctx, file, endpoint, method, headers, data)
	if err != nil && provider == "xai" {
		raw, err = b.quotaCall(ctx, file, "https://cli-chat-proxy.grok.com/v1/billing", "GET", headers, "")
	}
	if err != nil {
		return nil, err
	}
	g := parse(raw)
	if provider == "xai" && !hasGroup(g, "monthly-billing") {
		if monthly, err := b.quotaCall(ctx, file, "https://cli-chat-proxy.grok.com/v1/billing", "GET", headers, ""); err == nil {
			for _, item := range parseXai(monthly) {
				if item.ID == "monthly-billing" {
					g = append(g, item)
				}
			}
		}
	}
	if len(g) == 0 {
		return nil, errors.New("quota not reported")
	}
	return g, nil
}
