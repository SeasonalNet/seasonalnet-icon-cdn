package cdn

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var allowedSizes = map[int]struct{}{16: {}, 24: {}, 32: {}, 48: {}, 64: {}, 96: {}, 128: {}}

type Server struct {
	config       Config
	renderer     *Renderer
	cache        *DiskCache
	cleanupTimer *time.Ticker
	cleanupStop  chan struct{}
	closeOnce    sync.Once
	ready        atomic.Bool
	flights      flightGroup
	metrics      metrics
}

type metrics struct {
	requests         atomic.Uint64
	cacheHits        atomic.Uint64
	cacheMisses      atomic.Uint64
	coalesced        atomic.Uint64
	renders          atomic.Uint64
	renderErrors     atomic.Uint64
	renderDurationNS atomic.Uint64
	cacheReadErrors  atomic.Uint64
	cacheWriteErrors atomic.Uint64
}

type renderFlight struct {
	done    chan struct{}
	data    []byte
	err     error
	waiters int
}

type flightGroup struct {
	mu sync.Mutex
	m  map[string]*renderFlight
}

type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code"`
}

type healthResponse struct {
	Status string `json:"status"`
}

type iconManifest struct {
	LucideVersion string   `json:"lucide_version"`
	Icons         []string `json:"icons"`
}

func NewServer(config Config, renderer *Renderer) (*Server, error) {
	cache := NewDiskCache(config.Cache)
	if err := cache.Ensure(); err != nil {
		return nil, fmt.Errorf("initialize cache: %w", err)
	}
	cleanupStop := make(chan struct{})
	server := &Server{config: config, renderer: renderer, cache: cache, cleanupStop: cleanupStop, flights: flightGroup{m: make(map[string]*renderFlight)}}
	server.ready.Store(true)
	if config.Cache.Cleanup.Enabled && config.Cache.Cleanup.OnStartup {
		go func() {
			stats, ok, err := cache.Cleanup("startup")
			if err != nil {
				log.Printf("[cdn] startup cache cleanup error: %v", err)
				return
			}
			if ok {
				removed := stats.RemovedTmp + stats.RemovedStale + stats.RemovedOverLimit
				if removed > 0 {
					log.Printf("[cdn] startup cache cleanup removed %d files (tmp=%d, stale=%d, over_limit=%d)", removed, stats.RemovedTmp, stats.RemovedStale, stats.RemovedOverLimit)
				}
			}
		}()
	}
	server.cleanupTimer = cache.StartCleanupTimer(log.Printf, cleanupStop)
	return server, nil
}

func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.ready.Store(false)
		if s.cleanupTimer != nil {
			s.cleanupTimer.Stop()
		}
		close(s.cleanupStop)
	})
}

func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/metrics" {
		s.metrics.requests.Add(1)
	}
	switch r.URL.Path {
	case "/health":
		if !s.requireReadOnlyMethod(w, r) {
			return
		}
		writeHealth(w, "ok")
	case "/ready":
		if !s.requireReadOnlyMethod(w, r) {
			return
		}
		if !s.ready.Load() {
			writeProblem(w, http.StatusServiceUnavailable, "not_ready", "Not ready", "The service is not ready.")
			return
		}
		writeJSON(w, http.StatusOK, healthResponse{Status: "ready"})
	case "/icons":
		if !s.requireReadOnlyMethod(w, r) {
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		writeJSON(w, http.StatusOK, iconManifest{LucideVersion: s.renderer.Version(), Icons: s.renderer.IconNames()})
	case "/metrics":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, "GET")
			return
		}
		s.serveMetrics(w)
	case "/icon":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w)
			return
		}
		s.serveIcon(w, r)
	default:
		writeProblem(w, http.StatusNotFound, "not_found", "Not found", "The requested endpoint does not exist.")
	}
}

func (s *Server) requireReadOnlyMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	methodNotAllowed(w, "GET")
	return false
}

func (s *Server) serveIcon(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	icon := strings.ToLower(strings.TrimSpace(query.Get("icon")))
	hexColor := strings.ToUpper(strings.TrimPrefix(query.Get("hex"), "#"))
	if query.Get("hex") == "" {
		hexColor = "FFFFFF"
	}
	if !iconNamePattern.MatchString(icon) {
		writeProblem(w, http.StatusBadRequest, "invalid_icon_name", "Invalid icon name", "Icon names must contain 1 to 64 lowercase letters, digits, or hyphens.")
		return
	}
	if !hexColorPattern.MatchString(hexColor) {
		writeProblem(w, http.StatusBadRequest, "invalid_hex_color", "Invalid hex color", "Expected six hexadecimal characters, with an optional leading #.")
		return
	}
	format := strings.ToLower(strings.TrimSpace(query.Get("format")))
	if format == "" {
		format = "png"
	}
	if format != "png" && format != "svg" {
		writeProblem(w, http.StatusBadRequest, "invalid_format", "Invalid format", "Format must be png or svg.")
		return
	}
	size := s.config.Render.Size
	if values, ok := query["size"]; ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(query.Get("size")))
		if err != nil || len(values) != 1 {
			writeProblem(w, http.StatusBadRequest, "invalid_size", "Invalid size", "Size must be one of 16, 24, 32, 48, 64, 96, or 128.")
			return
		}
		if _, allowed := allowedSizes[parsed]; !allowed {
			writeProblem(w, http.StatusBadRequest, "invalid_size", "Invalid size", "Size must be one of 16, 24, 32, 48, 64, 96, or 128.")
			return
		}
		size = parsed
	}
	if !s.renderer.HasIcon(icon) {
		writeProblem(w, http.StatusNotFound, "icon_not_found", "Icon not found", "No icon exists with the requested name.")
		return
	}

	data, cacheStatus := s.loadOrRender(icon, hexColor, size, format)
	if data == nil {
		writeProblem(w, http.StatusInternalServerError, "render_failed", "Icon render failed", "The icon could not be rendered.")
		return
	}
	digest := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	contentType := "image/png"
	if format == "svg" {
		contentType = "image/svg+xml"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", s.cache.CacheControl())
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Cache", cacheStatus)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (s *Server) loadOrRender(icon, color string, size int, format string) ([]byte, string) {
	data, err := s.cache.Read(icon, color, size, format)
	if err != nil {
		s.metrics.cacheReadErrors.Add(1)
		log.Printf("[cdn] cache read error icon=%s hex=%s: %v", icon, color, err)
	}
	if data != nil {
		s.metrics.cacheHits.Add(1)
		return data, "HIT"
	}
	s.metrics.cacheMisses.Add(1)
	key := fmt.Sprintf("%s-%s-%d-%s", icon, color, size, format)
	data, shared, err := s.flights.Do(key, func() ([]byte, error) {
		cached, readErr := s.cache.Read(icon, color, size, format)
		if readErr != nil {
			s.metrics.cacheReadErrors.Add(1)
			log.Printf("[cdn] cache read error icon=%s hex=%s: %v", icon, color, readErr)
		}
		if cached != nil {
			return cached, nil
		}
		started := time.Now()
		rendered, renderErr := s.renderer.Render(icon, color, size, format)
		s.metrics.renderDurationNS.Add(uint64(time.Since(started).Nanoseconds()))
		if renderErr != nil {
			s.metrics.renderErrors.Add(1)
			log.Printf("[cdn] render error icon=%s hex=%s: %v", icon, color, renderErr)
			return nil, renderErr
		}
		s.metrics.renders.Add(1)
		if rendered != nil {
			if writeErr := s.cache.Write(icon, color, size, format, rendered); writeErr != nil {
				s.metrics.cacheWriteErrors.Add(1)
				log.Printf("[cdn] cache write error: %v", writeErr)
			}
		}
		return rendered, nil
	})
	if err != nil {
		return nil, "MISS"
	}
	if shared {
		s.metrics.coalesced.Add(1)
		return data, "COALESCED"
	}
	return data, "MISS"
}

func (g *flightGroup) Do(key string, fn func() ([]byte, error)) ([]byte, bool, error) {
	g.mu.Lock()
	if call, ok := g.m[key]; ok {
		call.waiters++
		g.mu.Unlock()
		<-call.done
		return call.data, true, call.err
	}
	call := &renderFlight{done: make(chan struct{})}
	g.m[key] = call
	g.mu.Unlock()
	call.data, call.err = fn()
	g.mu.Lock()
	delete(g.m, key)
	close(call.done)
	g.mu.Unlock()
	return call.data, false, call.err
}

func (s *Server) serveMetrics(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(w, "# HELP seasonalnet_icon_cdn_requests_total Requests received, excluding metrics scrapes.\n# TYPE seasonalnet_icon_cdn_requests_total counter\nseasonalnet_icon_cdn_requests_total %d\n", s.metrics.requests.Load())
	_, _ = fmt.Fprintf(w, "# HELP seasonalnet_icon_cdn_cache_hits_total Cached responses served.\n# TYPE seasonalnet_icon_cdn_cache_hits_total counter\nseasonalnet_icon_cdn_cache_hits_total %d\n", s.metrics.cacheHits.Load())
	_, _ = fmt.Fprintf(w, "# HELP seasonalnet_icon_cdn_cache_misses_total Cache misses observed.\n# TYPE seasonalnet_icon_cdn_cache_misses_total counter\nseasonalnet_icon_cdn_cache_misses_total %d\n", s.metrics.cacheMisses.Load())
	_, _ = fmt.Fprintf(w, "# HELP seasonalnet_icon_cdn_coalesced_requests_total Requests sharing an in-flight render.\n# TYPE seasonalnet_icon_cdn_coalesced_requests_total counter\nseasonalnet_icon_cdn_coalesced_requests_total %d\n", s.metrics.coalesced.Load())
	_, _ = fmt.Fprintf(w, "# HELP seasonalnet_icon_cdn_renders_total Render attempts completed successfully.\n# TYPE seasonalnet_icon_cdn_renders_total counter\nseasonalnet_icon_cdn_renders_total %d\n", s.metrics.renders.Load())
	_, _ = fmt.Fprintf(w, "# HELP seasonalnet_icon_cdn_render_errors_total Render failures.\n# TYPE seasonalnet_icon_cdn_render_errors_total counter\nseasonalnet_icon_cdn_render_errors_total %d\n", s.metrics.renderErrors.Load())
	_, _ = fmt.Fprintf(w, "# HELP seasonalnet_icon_cdn_render_duration_seconds_sum Total render duration.\n# TYPE seasonalnet_icon_cdn_render_duration_seconds_sum counter\nseasonalnet_icon_cdn_render_duration_seconds_sum %.9f\n", float64(s.metrics.renderDurationNS.Load())/1e9)
	_, _ = fmt.Fprintf(w, "# HELP seasonalnet_icon_cdn_cache_read_errors_total Cache read failures.\n# TYPE seasonalnet_icon_cdn_cache_read_errors_total counter\nseasonalnet_icon_cdn_cache_read_errors_total %d\n", s.metrics.cacheReadErrors.Load())
	_, _ = fmt.Fprintf(w, "# HELP seasonalnet_icon_cdn_cache_write_errors_total Cache write failures.\n# TYPE seasonalnet_icon_cdn_cache_write_errors_total counter\nseasonalnet_icon_cdn_cache_write_errors_total %d\n", s.metrics.cacheWriteErrors.Load())
}

func methodNotAllowed(w http.ResponseWriter, allow ...string) {
	allowed := "GET, HEAD"
	if len(allow) > 0 {
		allowed = allow[0]
	}
	w.Header().Set("Allow", allowed)
	writeProblem(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "Use GET or HEAD for this endpoint.")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeHealth(w http.ResponseWriter, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `{"status":"%s"}`, status)
}

func writeProblem(w http.ResponseWriter, status int, code, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{Type: "about:blank", Title: title, Status: status, Detail: detail, Code: code})
}

func matchesETag(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}
