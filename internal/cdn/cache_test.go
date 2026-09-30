package cdn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiskCacheAtomicLayoutAndCleanup(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig().Cache
	cfg.Dir = root
	cfg.ResolvedNamespace = "lucide-1.48.0"
	cfg.VersionedDir = filepath.Join(root, cfg.ResolvedNamespace, "size-64")
	cfg.Cleanup.MaxAgeDays = intPointer(1)
	cfg.Cleanup.MaxFiles = intPointer(1)
	cfg.Cleanup.MaxBytes = intPointer(4)
	cache := NewDiskCache(cfg)
	if err := cache.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := cache.Write("siren", "FF0000", 64, "png", []byte("pngdata")); err != nil {
		t.Fatal(err)
	}
	path := cache.Path("siren", "FF0000", 64, "png")
	data, err := cache.Read("siren", "FF0000", 64, "png")
	if err != nil || string(data) != "pngdata" {
		t.Fatalf("read cache: data=%q err=%v", data, err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	stats, ok, err := cache.Cleanup("test")
	if err != nil || !ok {
		t.Fatalf("cleanup: ok=%v err=%v", ok, err)
	}
	if stats.RemovedStale != 1 {
		t.Fatalf("removed stale=%d, want 1", stats.RemovedStale)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale entry remains: %v", err)
	}
}

func TestCleanupSkipsOverlappingRun(t *testing.T) {
	cfg := DefaultConfig().Cache
	cfg.Dir = filepath.Join(t.TempDir(), "cache")
	cfg.VersionedDir = filepath.Join(cfg.Dir, "lucide", "size-64")
	cache := NewDiskCache(cfg)
	cache.cleanupMu.Lock()
	cache.cleanupRunning = true
	cache.cleanupMu.Unlock()
	if _, ok, err := cache.Cleanup("test"); err != nil || ok {
		t.Fatalf("overlap: ok=%v err=%v, want skipped", ok, err)
	}
	cache.cleanupMu.Lock()
	cache.cleanupRunning = false
	cache.cleanupMu.Unlock()
}

func TestDiskCacheReadAndWriteFailures(t *testing.T) {
	cfg := DefaultConfig().Cache
	root := t.TempDir()
	cfg.Dir = root
	cfg.ResolvedNamespace = "lucide-test"
	cfg.VersionedDir = filepath.Join(root, cfg.ResolvedNamespace, "size-64")
	cache := NewDiskCache(cfg)
	if err := cache.Ensure(); err != nil {
		t.Fatal(err)
	}

	data, err := cache.Read("missing", "FFFFFF", 64, "png")
	if err != nil || data != nil {
		t.Fatalf("missing read = %q, %v; want nil, nil", data, err)
	}
	target := cache.Path("directory", "FFFFFF", 64, "png")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err = cache.Read("directory", "FFFFFF", 64, "png")
	if err == nil || data != nil {
		t.Fatalf("reading a directory = %q, %v; want nil data and error", data, err)
	}

	blockedRoot := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedRoot, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Dir = blockedRoot
	cfg.VersionedDir = filepath.Join(blockedRoot, cfg.ResolvedNamespace, "size-64")
	blocked := NewDiskCache(cfg)
	if err := blocked.Write("siren", "FFFFFF", 64, "png", []byte("x")); err == nil {
		t.Fatal("writing under a file should return an error")
	}
}

func TestCleanupNoopModesAndPruning(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		cfg := DefaultConfig().Cache
		cfg.Dir = filepath.Join(t.TempDir(), "missing")
		cfg.Cleanup.Enabled = false
		stats, ok, err := NewDiskCache(cfg).Cleanup("disabled")
		if err != nil || !ok || stats.Reason != "disabled" || stats.Scanned != 0 {
			t.Fatalf("cleanup disabled: stats=%+v ok=%v err=%v", stats, ok, err)
		}
	})

	t.Run("missing cache root", func(t *testing.T) {
		cfg := DefaultConfig().Cache
		cfg.Dir = filepath.Join(t.TempDir(), "missing")
		stats, ok, err := NewDiskCache(cfg).Cleanup("missing")
		if err != nil || !ok || stats.Scanned != 0 {
			t.Fatalf("cleanup missing: stats=%+v ok=%v err=%v", stats, ok, err)
		}
	})

	t.Run("stale temporary files and oldest over-limit pngs", func(t *testing.T) {
		root := t.TempDir()
		cfg := DefaultConfig().Cache
		cfg.Dir = root
		cfg.ResolvedNamespace = "lucide-test"
		cfg.VersionedDir = filepath.Join(root, cfg.ResolvedNamespace, "size-64")
		cfg.Cleanup.MaxAgeDays = nil
		cfg.Cleanup.MaxFiles = intPointer(1)
		cfg.Cleanup.MaxBytes = intPointer(100)
		cfg.Cleanup.TmpMaxAgeMinutes = 10
		cache := NewDiskCache(cfg)
		if err := cache.Ensure(); err != nil {
			t.Fatal(err)
		}
		oldPNG := cache.Path("old", "FFFFFF", 64, "png")
		newPNG := cache.Path("new", "FFFFFF", 64, "png")
		oldTmp := filepath.Join(cfg.VersionedDir, "abandoned.tmp")
		newTmp := filepath.Join(cfg.VersionedDir, "recent.tmp")
		textFile := filepath.Join(cfg.VersionedDir, "readme.txt")
		for name, data := range map[string]string{oldPNG: "old", newPNG: "new", oldTmp: "tmp", newTmp: "tmp", textFile: "ignore"} {
			if err := os.WriteFile(name, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		old := time.Now().Add(-20 * time.Minute)
		if err := os.Chtimes(oldPNG, old, old); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(oldTmp, old, old); err != nil {
			t.Fatal(err)
		}
		newer := time.Now().Add(-time.Minute)
		if err := os.Chtimes(newPNG, newer, newer); err != nil {
			t.Fatal(err)
		}
		stats, ok, err := cache.Cleanup("prune")
		if err != nil || !ok {
			t.Fatalf("cleanup: stats=%+v ok=%v err=%v", stats, ok, err)
		}
		if stats.RemovedTmp != 1 || stats.RemovedOverLimit != 1 || stats.RemainingFiles != 1 || stats.RemainingBytes != 3 {
			t.Fatalf("cleanup stats = %+v", stats)
		}
		for _, name := range []string{oldPNG, oldTmp} {
			if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("removed file %s remains: %v", name, err)
			}
		}
		for _, name := range []string{newPNG, newTmp, textFile} {
			if _, err := os.Stat(name); err != nil {
				t.Errorf("cleanup unexpectedly removed %s: %v", name, err)
			}
		}
	})
}

func TestCacheControlOmitsImmutableWhenDisabled(t *testing.T) {
	cfg := DefaultConfig().Cache
	cfg.HTTPMaxAgeSeconds = 30
	cfg.Immutable = false
	if got, want := NewDiskCache(cfg).CacheControl(), "public, max-age=30"; got != want {
		t.Fatalf("cache-control = %q, want %q", got, want)
	}
}

func TestCleanupTimerCanBeDisabledAndStopped(t *testing.T) {
	cfg := DefaultConfig().Cache
	cfg.Dir = t.TempDir()
	cfg.ResolvedNamespace = "lucide-test"
	cfg.VersionedDir = filepath.Join(cfg.Dir, cfg.ResolvedNamespace, "size-64")
	cfg.Cleanup.Enabled = false
	cache := NewDiskCache(cfg)
	if ticker := cache.StartCleanupTimer(func(string, ...any) {}, nil); ticker != nil {
		t.Fatal("disabled timer should be nil")
	}

	cfg.Cleanup.Enabled = true
	cfg.Cleanup.IntervalMS = 10
	cfg.Cleanup.MaxFiles = intPointer(0)
	cache = NewDiskCache(cfg)
	if err := cache.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache.Path("old", "FFFFFF", 64, "png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	logged := make(chan string, 1)
	ticker := cache.StartCleanupTimer(func(format string, args ...any) {
		logged <- fmt.Sprintf(format, args...)
		close(stop)
	}, stop)
	defer ticker.Stop()
	select {
	case message := <-logged:
		if !strings.Contains(message, "cache cleanup removed") {
			t.Fatalf("timer log = %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduled cleanup did not run")
	}
}
