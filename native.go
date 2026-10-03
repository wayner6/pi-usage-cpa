package main

/*
#include <stdlib.h>
#include <stdint.h>
typedef struct { void *ptr; size_t len; } cliproxy_buffer;
typedef int (*host_call)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*host_free)(void*, size_t);
typedef struct { uint32_t abi_version; void *host_ctx; host_call call; host_free free_buffer; } cliproxy_host_api;
typedef int (*plugin_call)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*plugin_free)(void*, size_t);
typedef void (*plugin_shutdown)(void);
typedef struct { uint32_t abi_version; plugin_call call; plugin_free free_buffer; plugin_shutdown shutdown; } cliproxy_plugin_api;
extern int piUsageCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void piUsageFree(void*, size_t);
extern void piUsageShutdown(void);
*/
import "C"
import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unsafe"
)

// Release builds set this from the Git tag with -ldflags -X.
var pluginVersion = "0.1.0-dev"
var runtimeMu sync.Mutex
var runtimeBridge *bridge

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, api *C.cliproxy_plugin_api) C.int {
	if host == nil || host.abi_version != 1 || api == nil {
		return 1
	}
	api.abi_version = 1
	api.call = C.plugin_call(C.piUsageCall)
	api.free_buffer = C.plugin_free(C.piUsageFree)
	api.shutdown = C.plugin_shutdown(C.piUsageShutdown)
	return 0
}

//export piUsageFree
func piUsageFree(ptr unsafe.Pointer, length C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export piUsageShutdown
func piUsageShutdown() { runtimeMu.Lock(); runtimeBridge = nil; runtimeMu.Unlock() }

//export piUsageCall
func piUsageCall(method *C.char, request *C.uint8_t, length C.size_t, response *C.cliproxy_buffer) C.int {
	if response == nil {
		return 1
	}
	response.ptr = nil
	response.len = 0
	if method == nil || length > 4*1024*1024 {
		return 1
	}
	var raw []byte
	if length > 0 {
		if request == nil {
			return 1
		}
		raw = C.GoBytes(unsafe.Pointer(request), C.int(length))
	}
	result := dispatch(C.GoString(method), raw)
	response.ptr = C.CBytes(result)
	response.len = C.size_t(len(result))
	return 0
}
func envelope(result any) []byte {
	raw, _ := json.Marshal(map[string]any{"ok": true, "result": result})
	return raw
}
func dispatch(method string, raw []byte) []byte {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		runtimeMu.Lock()
		b, err := fromEnvironment()
		runtimeBridge = b
		runtimeMu.Unlock()
		if err != nil {
			return []byte(`{"ok":false,"error":{"code":"configuration","message":"server-only loopback management configuration required"}}`)
		}
		return envelope(map[string]any{
			"schema_version": 1,
			"metadata": map[string]any{
				"Name":             "pi-usage-cpa",
				"Version":          pluginVersion,
				"Author":           "pi-usage-cpa contributors",
				"GitHubRepository": "https://github.com/wayner6/pi-usage-cpa",
				"ConfigFields":     []any{},
			},
			"capabilities": map[string]any{"management_api": true},
		})
	case "management.register":
		return envelope(map[string]any{"resources": []any{map[string]string{"Path": "/usage"}, map[string]string{"Path": "/capabilities"}, map[string]string{"Path": "/well-known"}}})
	case "management.handle":
		var req struct {
			Method  string
			Path    string
			Headers http.Header
			Query   url.Values
		}
		if json.Unmarshal(raw, &req) != nil {
			return resourceResponse([]byte(`{"error":"invalid request"}`), 400)
		}
		runtimeMu.Lock()
		b := runtimeBridge
		runtimeMu.Unlock()
		if b == nil {
			return resourceResponse([]byte(`{"error":"not configured"}`), 503)
		}
		path := req.Path
		const prefix = "/v0/resource/plugins/pi-usage-cpa"
		if strings.HasPrefix(path, prefix+"/") {
			path = strings.TrimPrefix(path, prefix)
		}
		body, status := b.handle(req.Method, path, req.Headers, req.Query)
		return resourceResponse(body, status)
	default:
		return []byte(`{"ok":false,"error":{"code":"unsupported","message":"unsupported operation"}}`)
	}
}
func resourceResponse(body []byte, status int) []byte {
	return envelope(struct {
		StatusCode int
		Headers    http.Header
		Body       []byte
	}{status, http.Header{"Content-Type": []string{"application/json"}, "Cache-Control": []string{"no-store"}, "X-Content-Type-Options": []string{"nosniff"}}, body})
}
