package cdn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigDefaultsAndOverrides(t *testing.T) {
	root := t.TempDir()
	configFile := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configFile, []byte("render:\n  size: 48\ncache:\n  cleanup:\n    max_files: null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(root, map[string]string{
		"CDN_PORT":            "3701junk",
		"CDN_CACHE_IMMUTABLE": "off",
		"CDN_CACHE_DIR":       "cache-dir",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 3701 || cfg.Render.Size != 48 {
		t.Fatalf("unexpected config values: %+v", cfg)
	}
	if cfg.Cache.Immutable {
		t.Fatal("CDN_CACHE_IMMUTABLE override was ignored")
	}
	if cfg.Cache.Cleanup.MaxFiles != nil {
		t.Fatal("explicit null max_files should disable the limit")
	}
	wantDir := filepath.Join(root, "cache-dir", "lucide-"+cfg.LucideVersion, "size-48")
	if cfg.Cache.VersionedDir != wantDir {
		t.Fatalf("versioned dir = %q, want %q", cfg.Cache.VersionedDir, wantDir)
	}
	if !cfg.ConfigExists || cfg.ConfigPath != configFile {
		t.Fatalf("config file metadata = %+v", cfg)
	}
}

func TestEnvBooleanRejectsUnknownValues(t *testing.T) {
	_, err := LoadConfig(t.TempDir(), map[string]string{"CDN_CACHE_IMMUTABLE": "sometimes"})
	if err == nil {
		t.Fatal("expected invalid boolean to fail")
	}
}

func TestLoadConfigWithoutFileUsesDefaults(t *testing.T) {
	root := t.TempDir()
	cfg, err := LoadConfig(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigExists || cfg.ConfigPath != filepath.Join(root, "config.yaml") {
		t.Fatalf("missing config metadata = exists:%v path:%q", cfg.ConfigExists, cfg.ConfigPath)
	}
	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 3600 || cfg.Render.Size != 64 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadConfigRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		env        map[string]string
		wantSubstr string
	}{
		{name: "empty root", wantSubstr: "application root", env: map[string]string{"CDN_CONFIG": "config.yaml"}},
		{name: "malformed yaml", config: "server: [", wantSubstr: "failed to parse"},
		{name: "wrong yaml type", config: "server:\n  port: nope\n", wantSubstr: "failed to parse"},
		{name: "zero port", config: "server:\n  port: 0\n", wantSubstr: "server.port"},
		{name: "negative size", config: "render:\n  size: -1\n", wantSubstr: "render.size"},
		{name: "empty host", config: "server:\n  host: ''\n", wantSubstr: "server.host"},
		{name: "negative max age", config: "cache:\n  cleanup:\n    max_age_days: -1\n", wantSubstr: "max_age_days"},
		{name: "negative max files", config: "cache:\n  cleanup:\n    max_files: -1\n", wantSubstr: "max_files"},
		{name: "negative max bytes", config: "cache:\n  cleanup:\n    max_bytes: -1\n", wantSubstr: "max_bytes"},
		{name: "negative max age seconds", config: "cache:\n  http_max_age_seconds: -1\n", wantSubstr: "http_max_age_seconds"},
		{name: "negative cleanup interval", config: "cache:\n  cleanup:\n    interval_ms: -1\n", wantSubstr: "cleanup intervals"},
		{name: "negative temp age", config: "cache:\n  cleanup:\n    tmp_max_age_minutes: -1\n", wantSubstr: "cleanup intervals"},
		{name: "invalid port env", env: map[string]string{"CDN_PORT": "port"}, wantSubstr: "CDN_PORT"},
		{name: "integer overflow env", env: map[string]string{"CDN_SIZE": "999999999999999999999999999999"}, wantSubstr: "CDN_SIZE"},
		{name: "invalid nullable env", env: map[string]string{"CDN_CACHE_MAX_FILES": "many"}, wantSubstr: "CDN_CACHE_MAX_FILES"},
		{name: "invalid bool env", env: map[string]string{"CDN_CACHE_IMMUTABLE": "sometimes"}, wantSubstr: "CDN_CACHE_IMMUTABLE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.config != "" {
				if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(test.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if test.name == "empty root" {
				_, err := LoadConfig("", test.env)
				if err == nil || !strings.Contains(err.Error(), test.wantSubstr) {
					t.Fatalf("error = %v, want %q", err, test.wantSubstr)
				}
				return
			}
			_, err := LoadConfig(root, test.env)
			if err == nil || !strings.Contains(err.Error(), test.wantSubstr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantSubstr)
			}
		})
	}
}

func TestLoadConfigReadErrorAndRelativePaths(t *testing.T) {
	t.Run("config path is a directory", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "config.yaml"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig(root, nil)
		if err == nil || !strings.Contains(err.Error(), "read config") {
			t.Fatalf("error = %v, want config read error", err)
		}
	})

	t.Run("environment paths resolve", func(t *testing.T) {
		root := t.TempDir()
		configDir := filepath.Join(root, "settings")
		if err := os.Mkdir(configDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "custom.yaml"), []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(root, map[string]string{
			"CDN_CONFIG":    "settings/custom.yaml",
			"CDN_CACHE_DIR": "cache",
			"CDN_ICONS_DIR": "icons",
		})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ConfigPath != filepath.Join(configDir, "custom.yaml") || cfg.Cache.Dir != filepath.Join(configDir, "cache") || cfg.Render.IconsDir != filepath.Join(root, "icons") {
			t.Fatalf("resolved paths: config=%q cache=%q icons=%q", cfg.ConfigPath, cfg.Cache.Dir, cfg.Render.IconsDir)
		}
	})
}

func TestLoadConfigAppliesAllEnvironmentOverrides(t *testing.T) {
	root := t.TempDir()
	cfg, err := LoadConfig(root, map[string]string{
		"CDN_HOST":                       "0.0.0.0",
		"CDN_PORT":                       "3777tail",
		"CDN_SIZE":                       "32px",
		"CDN_ICONS_DIR":                  "./icons",
		"CDN_CACHE_DIR":                  filepath.Join(root, "cache"),
		"CDN_CACHE_NAMESPACE":            "release/one",
		"CDN_CACHE_HTTP_MAX_AGE_SECONDS": "3600seconds",
		"CDN_CACHE_IMMUTABLE":            "yes",
		"CDN_CACHE_CLEANUP_ENABLED":      "off",
		"CDN_CACHE_CLEAN_ON_STARTUP":     "false",
		"CDN_CACHE_CLEAN_INTERVAL_MS":    "0",
		"CDN_CACHE_MAX_AGE_DAYS":         "null",
		"CDN_CACHE_MAX_FILES":            "17",
		"CDN_CACHE_MAX_BYTES":            "null",
		"CDN_CACHE_TMP_MAX_AGE_MINUTES":  "5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Host != "0.0.0.0" || cfg.Server.Port != 3777 || cfg.Render.Size != 32 || cfg.Render.IconsDir != filepath.Join(root, "icons") {
		t.Fatalf("server/render overrides: %+v %+v", cfg.Server, cfg.Render)
	}
	if cfg.Cache.Dir != filepath.Join(root, "cache") || cfg.Cache.ResolvedNamespace != "release-one" || cfg.Cache.HTTPMaxAgeSeconds != 3600 || !cfg.Cache.Immutable {
		t.Fatalf("cache overrides: %+v", cfg.Cache)
	}
	if cfg.Cache.Cleanup.Enabled || cfg.Cache.Cleanup.OnStartup || cfg.Cache.Cleanup.IntervalMS != 0 || cfg.Cache.Cleanup.MaxAgeDays != nil || cfg.Cache.Cleanup.MaxFiles == nil || *cfg.Cache.Cleanup.MaxFiles != 17 || cfg.Cache.Cleanup.MaxBytes != nil || cfg.Cache.Cleanup.TmpMaxAgeMinutes != 5 {
		t.Fatalf("cleanup overrides: %+v", cfg.Cache.Cleanup)
	}
}

func TestSanitizeNamespace(t *testing.T) {
	tests := map[string]string{
		" ^version/1.0+build ": "version-1.0-build",
		"---":                  "default",
		"A_b.c-9":              "A_b.c-9",
	}
	for input, want := range tests {
		if got := sanitizeNamespace(input); got != want {
			t.Errorf("sanitizeNamespace(%q) = %q, want %q", input, got, want)
		}
	}
}
