package cdn

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerRejectsBadRequestsWithoutRendering(t *testing.T) {
	service := newTestServer(t, "")
	tests := []struct {
		path   string
		status int
		code   string
	}{
		{path: "/missing", status: http.StatusNotFound, code: "not_found"},
		{path: "/icon?icon=bad_name&hex=FF0000", status: http.StatusBadRequest, code: "invalid_icon_name"},
		{path: "/icon?icon=siren&hex=xyz", status: http.StatusBadRequest, code: "invalid_hex_color"},
		{path: "/icon?icon=siren&hex=FF0000&format=gif", status: http.StatusBadRequest, code: "invalid_format"},
		{path: "/icon?icon=siren&hex=FF0000&size=abc", status: http.StatusBadRequest, code: "invalid_size"},
		{path: "/icon?icon=siren&hex=FF0000&size=17", status: http.StatusBadRequest, code: "invalid_size"},
		{path: "/icon?icon=siren&hex=FF0000&size=16&size=32", status: http.StatusBadRequest, code: "invalid_size"},
		{path: "/icon?icon=not-an-icon&hex=FF0000", status: http.StatusNotFound, code: "icon_not_found"},
	}
	for _, test := range tests {
		t.Run(test.code+" "+test.path, func(t *testing.T) {
			response := serveTestRequest(service, http.MethodGet, test.path, nil)
			defer closeResponse(t, response)
			var problem Problem
			if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.status || problem.Status != test.status || problem.Code != test.code || response.Header.Get("Content-Type") != "application/problem+json" {
				t.Fatalf("response status=%d problem=%+v content-type=%q", response.StatusCode, problem, response.Header.Get("Content-Type"))
			}
		})
	}
	if service.metrics.renders.Load() != 0 {
		t.Fatalf("invalid requests caused %d renders", service.metrics.renders.Load())
	}
}

func TestServerMethodRestrictions(t *testing.T) {
	service := newTestServer(t, "")
	tests := []struct {
		method string
		path   string
		allow  string
	}{
		{method: http.MethodPost, path: "/health", allow: "GET"},
		{method: http.MethodHead, path: "/ready", allow: "GET"},
		{method: http.MethodPost, path: "/icons", allow: "GET"},
		{method: http.MethodHead, path: "/metrics", allow: "GET"},
		{method: http.MethodPost, path: "/icon?icon=siren", allow: "GET, HEAD"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := serveTestRequest(service, test.method, test.path, nil)
			defer closeResponse(t, response)
			var problem Problem
			if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != test.allow || problem.Code != "method_not_allowed" {
				t.Fatalf("response status=%d allow=%q problem=%+v", response.StatusCode, response.Header.Get("Allow"), problem)
			}
		})
	}
}

func TestServerCacheFailuresDoNotFailRenderedResponse(t *testing.T) {
	service := newTestServer(t, "")
	cacheTarget := service.cache.Path("siren", "FF0000", 64, "png")
	if err := os.MkdirAll(cacheTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheTarget, "keep"), []byte("block rename"), 0o600); err != nil {
		t.Fatal(err)
	}
	response := serveTestRequest(service, http.MethodGet, "/icon?icon=siren&hex=FF0000", nil)
	defer closeResponse(t, response)
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("response status=%d x-cache=%q", response.StatusCode, response.Header.Get("X-Cache"))
	}
	if service.metrics.cacheReadErrors.Load() == 0 || service.metrics.cacheWriteErrors.Load() == 0 || service.metrics.renders.Load() != 1 {
		t.Fatalf("cache metrics: read_errors=%d write_errors=%d renders=%d", service.metrics.cacheReadErrors.Load(), service.metrics.cacheWriteErrors.Load(), service.metrics.renders.Load())
	}
}

func TestServerRenderFailureReturnsProblemAndKeepsServing(t *testing.T) {
	iconsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(iconsDir, "siren.svg"), []byte("<svg><broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newTestServer(t, iconsDir)
	response := serveTestRequest(service, http.MethodGet, "/icon?icon=siren&hex=FF0000", nil)
	var problem Problem
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	closeResponse(t, response)
	if response.StatusCode != http.StatusInternalServerError || problem.Code != "render_failed" {
		t.Fatalf("render failure response: status=%d problem=%+v", response.StatusCode, problem)
	}
	if service.metrics.renderErrors.Load() != 1 || service.metrics.renders.Load() != 0 {
		t.Fatalf("render metrics: errors=%d success=%d", service.metrics.renderErrors.Load(), service.metrics.renders.Load())
	}
	response = serveTestRequest(service, http.MethodGet, "/health", nil)
	defer closeResponse(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("service stopped after render error: status=%d", response.StatusCode)
	}
}

func TestReadOnlyMethodsAndConditionalTags(t *testing.T) {
	tests := []struct {
		header string
		want   bool
	}{
		{header: `"abc"`, want: true},
		{header: `W/"abc"`, want: true},
		{header: `"other", W/"abc"`, want: true},
		{header: "*", want: true},
		{header: `"other"`, want: false},
		{header: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.header, func(t *testing.T) {
			if got := matchesETag(test.header, `"abc"`); got != test.want {
				t.Fatalf("matchesETag(%q) = %v, want %v", test.header, got, test.want)
			}
		})
	}
}

func TestServerServesCacheHitAfterFirstRender(t *testing.T) {
	service := newTestServer(t, "")
	first := serveTestRequest(service, http.MethodGet, "/icon?icon=siren&hex=FF0000", nil)
	firstETag := first.Header.Get("ETag")
	closeResponse(t, first)
	second := serveTestRequest(service, http.MethodGet, "/icon?icon=siren&hex=FF0000", nil)
	defer closeResponse(t, second)
	if second.Header.Get("X-Cache") != "HIT" || second.Header.Get("ETag") != firstETag || service.metrics.renders.Load() != 1 {
		t.Fatalf("second response x-cache=%q etag=%q renders=%d", second.Header.Get("X-Cache"), second.Header.Get("ETag"), service.metrics.renders.Load())
	}
}

func TestNewServerRejectsUnwritableCachePath(t *testing.T) {
	cfg := DefaultConfig()
	filePath := filepath.Join(t.TempDir(), "cache-is-a-file")
	if err := os.WriteFile(filePath, []byte("block directory creation"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Cache.Dir = filePath
	cfg.Cache.VersionedDir = filepath.Join(filePath, cfg.Cache.ResolvedNamespace, "size-64")
	if _, err := NewServer(cfg, &Renderer{}); err == nil || !strings.Contains(err.Error(), "initialize cache") {
		t.Fatalf("NewServer error = %v, want cache initialization failure", err)
	}
}

func TestReadinessFailsAfterServerClose(t *testing.T) {
	service := newTestServer(t, "")
	service.Close()
	response := serveTestRequest(service, http.MethodGet, "/ready", nil)
	defer closeResponse(t, response)
	var problem Problem
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable || problem.Code != "not_ready" {
		t.Fatalf("readiness response status=%d problem=%+v", response.StatusCode, problem)
	}
}

func TestFlightGroupPropagatesErrorAndAllowsRetry(t *testing.T) {
	group := flightGroup{m: make(map[string]*renderFlight)}
	wantErr := fmt.Errorf("render failed")
	if _, shared, err := group.Do("key", func() ([]byte, error) { return nil, wantErr }); shared || err != wantErr {
		t.Fatalf("first call: shared=%v err=%v", shared, err)
	}
	data, shared, err := group.Do("key", func() ([]byte, error) { return []byte("recovered"), nil })
	if shared || err != nil || string(data) != "recovered" {
		t.Fatalf("retry: data=%q shared=%v err=%v", data, shared, err)
	}
}

func newTestServer(t *testing.T, iconsDir string) *Server {
	t.Helper()
	renderer, err := NewRenderer(iconsDir)
	if err != nil {
		t.Fatalf("initialize renderer: %v", err)
	}
	service, err := NewServer(testConfig(t), renderer)
	if err != nil {
		t.Fatalf("initialize server: %v", err)
	}
	t.Cleanup(func() {
		service.Close()
		if err := renderer.Close(); err != nil {
			t.Errorf("close renderer: %v", err)
		}
	})
	return service
}

func serveTestRequest(service *Server, method, path string, headers http.Header) *http.Response {
	request := httptest.NewRequest(method, "http://cdn.test"+path, nil)
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	recorder := httptest.NewRecorder()
	service.Handler().ServeHTTP(recorder, request)
	return recorder.Result()
}
