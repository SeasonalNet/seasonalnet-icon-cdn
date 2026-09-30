package cdn

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExistingAndAddedHTTPContracts(t *testing.T) {
	config := testConfig(t)
	renderer, err := NewRenderer("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("close renderer: %v", err)
		}
	}()
	service, err := NewServer(config, renderer)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	call := func(method, path string, headers http.Header) *http.Response {
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

	response := call(http.MethodGet, "/health", nil)
	body, _ := io.ReadAll(response.Body)
	closeResponse(t, response)
	if response.StatusCode != http.StatusOK || string(body) != `{"status":"ok"}` {
		t.Fatalf("health response: %d %q", response.StatusCode, body)
	}

	iconURL := "/icon?icon=siren&hex=FF0000"
	response = call(http.MethodGet, iconURL, nil)
	firstBody, _ := io.ReadAll(response.Body)
	firstETag := response.Header.Get("ETag")
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/png" || len(firstBody) == 0 || firstETag == "" {
		t.Fatalf("icon response: status=%d content-type=%q bytes=%d etag=%q", response.StatusCode, response.Header.Get("Content-Type"), len(firstBody), firstETag)
	}
	closeResponse(t, response)

	headers := make(http.Header)
	headers.Set("If-None-Match", "W/"+firstETag)
	response = call(http.MethodGet, iconURL, headers)
	closeResponse(t, response)
	if response.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional response status=%d", response.StatusCode)
	}

	response = call(http.MethodHead, iconURL, nil)
	if response.StatusCode != http.StatusOK || response.ContentLength != int64(len(firstBody)) {
		t.Fatalf("HEAD status=%d content-length=%d want=%d", response.StatusCode, response.ContentLength, len(firstBody))
	}
	closeResponse(t, response)

	response = call(http.MethodGet, "/icon?icon=siren&size=16&format=svg&hex=00ff00", nil)
	variant, _ := io.ReadAll(response.Body)
	closeResponse(t, response)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/svg+xml" || !strings.Contains(string(variant), `width="16"`) || !strings.Contains(string(variant), `height="16"`) || !strings.Contains(string(variant), `stroke-width="2"`) || !strings.Contains(string(variant), "#00FF00") {
		t.Fatalf("SVG variant: status=%d type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}

	response = call(http.MethodGet, "/icon?icon=does-not-exist&hex=FF0000", nil)
	var problem Problem
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	closeResponse(t, response)
	if response.StatusCode != http.StatusNotFound || response.Header.Get("Content-Type") != "application/problem+json" || problem.Code != "icon_not_found" {
		t.Fatalf("problem response: status=%d problem=%+v", response.StatusCode, problem)
	}

	response = call(http.MethodGet, "/icons", nil)
	var manifest iconManifest
	if err := json.NewDecoder(response.Body).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	closeResponse(t, response)
	if response.StatusCode != http.StatusOK || manifest.LucideVersion != renderer.Version() || len(manifest.Icons) < 1000 {
		t.Fatalf("manifest: status=%d version=%s count=%d", response.StatusCode, manifest.LucideVersion, len(manifest.Icons))
	}

	response = call(http.MethodGet, "/ready", nil)
	closeResponse(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("readiness status=%d", response.StatusCode)
	}
	response = call(http.MethodGet, "/metrics", nil)
	metricBody, _ := io.ReadAll(response.Body)
	closeResponse(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(metricBody), "seasonalnet_icon_cdn_renders_total") {
		t.Fatalf("metrics response: status=%d", response.StatusCode)
	}
}

func TestRenderEveryEmbeddedLucideIcon(t *testing.T) {
	renderer, err := NewRenderer("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("close renderer: %v", err)
		}
	}()
	for _, name := range renderer.IconNames() {
		data, err := renderer.Render(name, "FF0000", 64, "png")
		if err != nil {
			t.Errorf("render %s: %v", name, err)
			continue
		}
		if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
			t.Errorf("render %s returned invalid PNG data", name)
		}
	}
}

func TestFlightGroupCoalescesConcurrentRender(t *testing.T) {
	group := flightGroup{m: make(map[string]*renderFlight)}
	started := make(chan struct{})
	finish := make(chan struct{})
	var calls int
	var callsMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan bool, 2)
	fn := func() ([]byte, error) {
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		close(started)
		<-finish
		return []byte("same"), nil
	}
	go func() { defer wg.Done(); _, shared, _ := group.Do("key", fn); results <- shared }()
	<-started
	go func() { defer wg.Done(); _, shared, _ := group.Do("key", fn); results <- shared }()
	deadline := time.After(time.Second)
	for {
		group.mu.Lock()
		waiters := group.m["key"].waiters
		group.mu.Unlock()
		if waiters == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("second render did not join the in-flight render")
		default:
			runtime.Gosched()
		}
	}
	close(finish)
	wg.Wait()
	close(results)
	sharedCount := 0
	for shared := range results {
		if shared {
			sharedCount++
		}
	}
	if calls != 1 || sharedCount != 1 {
		t.Fatalf("calls=%d shared waiters=%d, want 1 and 1", calls, sharedCount)
	}
}

func testConfig(t *testing.T) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Cache.Dir = t.TempDir()
	cfg.Cache.ResolvedNamespace = "lucide-test"
	cfg.Cache.VersionedDir = cfg.Cache.Dir + "/lucide-test/size-64"
	cfg.Cache.Cleanup.Enabled = false
	return cfg
}

func closeResponse(t *testing.T, response *http.Response) {
	t.Helper()
	if err := response.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
}
