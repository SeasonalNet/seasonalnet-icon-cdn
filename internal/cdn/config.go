package cdn

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	Render RenderConfig `yaml:"render"`
	Cache  CacheConfig  `yaml:"cache"`

	ConfigPath    string `yaml:"-"`
	ConfigExists  bool   `yaml:"-"`
	LucideVersion string `yaml:"-"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type RenderConfig struct {
	Size     int    `yaml:"size"`
	IconsDir string `yaml:"icons_dir"`
}

type CacheConfig struct {
	Dir               string        `yaml:"dir"`
	Namespace         string        `yaml:"namespace"`
	HTTPMaxAgeSeconds int           `yaml:"http_max_age_seconds"`
	Immutable         bool          `yaml:"immutable"`
	Cleanup           CleanupConfig `yaml:"cleanup"`
	ResolvedNamespace string        `yaml:"-"`
	VersionedDir      string        `yaml:"-"`
}

type CleanupConfig struct {
	Enabled          bool `yaml:"enabled"`
	OnStartup        bool `yaml:"on_startup"`
	IntervalMS       int  `yaml:"interval_ms"`
	MaxAgeDays       *int `yaml:"max_age_days"`
	MaxFiles         *int `yaml:"max_files"`
	MaxBytes         *int `yaml:"max_bytes"`
	TmpMaxAgeMinutes int  `yaml:"tmp_max_age_minutes"`
}

func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{Host: "127.0.0.1", Port: 3600},
		Render: RenderConfig{Size: 64},
		Cache: CacheConfig{
			Dir: "./cache", Namespace: "auto", HTTPMaxAgeSeconds: 604800, Immutable: true,
			Cleanup: CleanupConfig{
				Enabled: true, OnStartup: true, IntervalMS: 3600000,
				MaxAgeDays: intPointer(180), MaxFiles: intPointer(25000), MaxBytes: intPointer(104857600),
				TmpMaxAgeMinutes: 30,
			},
		},
	}
}

func LoadConfig(appRoot string, env map[string]string) (Config, error) {
	if appRoot == "" {
		return Config{}, fmt.Errorf("application root is required")
	}
	if env == nil {
		env = map[string]string{}
	}
	configPath := env["CDN_CONFIG"]
	if configPath == "" {
		configPath = filepath.Join(appRoot, "config.yaml")
	} else if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(appRoot, configPath)
	}

	cfg := DefaultConfig()
	data, err := os.ReadFile(configPath)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("read config %s: %w", configPath, err)
	}
	if exists {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("failed to parse %s: %w", configPath, err)
		}
	}
	cfg.ConfigPath = configPath
	cfg.ConfigExists = exists
	if err := cfg.applyEnv(env); err != nil {
		return Config{}, err
	}

	if cfg.Server.Port < 1 {
		return Config{}, fmt.Errorf("server.port must be >= 1")
	}
	if cfg.Render.Size < 1 {
		return Config{}, fmt.Errorf("render.size must be >= 1")
	}
	if cfg.Cache.HTTPMaxAgeSeconds < 0 {
		return Config{}, fmt.Errorf("cache.http_max_age_seconds must be >= 0")
	}
	if cfg.Cache.Cleanup.IntervalMS < 0 || cfg.Cache.Cleanup.TmpMaxAgeMinutes < 0 {
		return Config{}, fmt.Errorf("cache cleanup intervals must be >= 0")
	}
	for name, value := range map[string]*int{
		"cache.cleanup.max_age_days": cfg.Cache.Cleanup.MaxAgeDays,
		"cache.cleanup.max_files":    cfg.Cache.Cleanup.MaxFiles,
		"cache.cleanup.max_bytes":    cfg.Cache.Cleanup.MaxBytes,
	} {
		if value != nil && *value < 0 {
			return Config{}, fmt.Errorf("%s must be >= 0", name)
		}
	}
	if cfg.Server.Host == "" {
		return Config{}, fmt.Errorf("server.host must not be empty")
	}

	if cfg.Cache.Dir != "" && !filepath.IsAbs(cfg.Cache.Dir) {
		cfg.Cache.Dir = filepath.Join(filepath.Dir(configPath), cfg.Cache.Dir)
	}
	if cfg.Render.IconsDir != "" && !filepath.IsAbs(cfg.Render.IconsDir) {
		cfg.Render.IconsDir = filepath.Join(appRoot, cfg.Render.IconsDir)
	}
	version := embeddedLucideVersion()
	cfg.LucideVersion = version
	namespace := cfg.Cache.Namespace
	if namespace == "auto" {
		namespace = "lucide-" + version
	}
	cfg.Cache.ResolvedNamespace = sanitizeNamespace(namespace)
	cfg.Cache.VersionedDir = filepath.Join(cfg.Cache.Dir, cfg.Cache.ResolvedNamespace, fmt.Sprintf("size-%d", cfg.Render.Size))
	return cfg, nil
}

func (cfg *Config) applyEnv(env map[string]string) error {
	if value, ok := env["CDN_HOST"]; ok {
		cfg.Server.Host = value
	}
	if value, ok := env["CDN_PORT"]; ok {
		if err := envInt(value, "CDN_PORT", &cfg.Server.Port); err != nil {
			return err
		}
	}
	if value, ok := env["CDN_SIZE"]; ok {
		if err := envInt(value, "CDN_SIZE", &cfg.Render.Size); err != nil {
			return err
		}
	}
	if value, ok := env["CDN_ICONS_DIR"]; ok {
		cfg.Render.IconsDir = value
	}
	if value, ok := env["CDN_CACHE_DIR"]; ok {
		cfg.Cache.Dir = value
	}
	if value, ok := env["CDN_CACHE_NAMESPACE"]; ok {
		cfg.Cache.Namespace = value
	}
	if value, ok := env["CDN_CACHE_HTTP_MAX_AGE_SECONDS"]; ok {
		if err := envInt(value, "CDN_CACHE_HTTP_MAX_AGE_SECONDS", &cfg.Cache.HTTPMaxAgeSeconds); err != nil {
			return err
		}
	}
	if value, ok := env["CDN_CACHE_IMMUTABLE"]; ok {
		if err := envBool(value, "CDN_CACHE_IMMUTABLE", &cfg.Cache.Immutable); err != nil {
			return err
		}
	}
	cleanup := &cfg.Cache.Cleanup
	if value, ok := env["CDN_CACHE_CLEANUP_ENABLED"]; ok {
		if err := envBool(value, "CDN_CACHE_CLEANUP_ENABLED", &cleanup.Enabled); err != nil {
			return err
		}
	}
	if value, ok := env["CDN_CACHE_CLEAN_ON_STARTUP"]; ok {
		if err := envBool(value, "CDN_CACHE_CLEAN_ON_STARTUP", &cleanup.OnStartup); err != nil {
			return err
		}
	}
	if value, ok := env["CDN_CACHE_CLEAN_INTERVAL_MS"]; ok {
		if err := envInt(value, "CDN_CACHE_CLEAN_INTERVAL_MS", &cleanup.IntervalMS); err != nil {
			return err
		}
	}
	if value, ok := env["CDN_CACHE_MAX_AGE_DAYS"]; ok {
		if err := envNullableInt(value, "CDN_CACHE_MAX_AGE_DAYS", &cleanup.MaxAgeDays); err != nil {
			return err
		}
	}
	if value, ok := env["CDN_CACHE_MAX_FILES"]; ok {
		if err := envNullableInt(value, "CDN_CACHE_MAX_FILES", &cleanup.MaxFiles); err != nil {
			return err
		}
	}
	if value, ok := env["CDN_CACHE_MAX_BYTES"]; ok {
		if err := envNullableInt(value, "CDN_CACHE_MAX_BYTES", &cleanup.MaxBytes); err != nil {
			return err
		}
	}
	if value, ok := env["CDN_CACHE_TMP_MAX_AGE_MINUTES"]; ok {
		if err := envInt(value, "CDN_CACHE_TMP_MAX_AGE_MINUTES", &cleanup.TmpMaxAgeMinutes); err != nil {
			return err
		}
	}
	return nil
}

var leadingInteger = regexp.MustCompile(`^\s*[+-]?\d+`)

func envInt(value, name string, target *int) error {
	match := leadingInteger.FindString(value)
	if match == "" {
		return fmt.Errorf("%s must be an integer", name)
	}
	n, err := strconv.Atoi(strings.TrimSpace(match))
	if err != nil {
		return fmt.Errorf("%s must be an integer", name)
	}
	*target = n
	return nil
}

func envNullableInt(value, name string, target **int) error {
	if strings.EqualFold(strings.TrimSpace(value), "null") {
		*target = nil
		return nil
	}
	var n int
	if err := envInt(value, name, &n); err != nil {
		return err
	}
	*target = &n
	return nil
}

func envBool(value, name string, target *bool) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		*target = true
	case "0", "false", "no", "off":
		*target = false
	default:
		return fmt.Errorf("%s must be a boolean", name)
	}
	return nil
}

func sanitizeNamespace(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "^"))
	var out strings.Builder
	lastDash := false
	for _, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
		if valid {
			out.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(out.String(), "-")
	if result == "" {
		return "default"
	}
	return result
}

func intPointer(value int) *int { return &value }
