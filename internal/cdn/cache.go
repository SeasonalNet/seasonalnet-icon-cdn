package cdn

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type DiskCache struct {
	config         CacheConfig
	root           string
	active         string
	cacheControl   string
	cleanupMu      sync.Mutex
	cleanupRunning bool
}

type CleanupStats struct {
	Reason           string `json:"reason"`
	Scanned          int    `json:"scanned"`
	RemovedTmp       int    `json:"removed_tmp"`
	RemovedStale     int    `json:"removed_stale"`
	RemovedOverLimit int    `json:"removed_over_limit"`
	RemainingFiles   int    `json:"remaining_files"`
	RemainingBytes   int64  `json:"remaining_bytes"`
}

type cacheFile struct {
	path  string
	size  int64
	mtime time.Time
}

func NewDiskCache(config CacheConfig) *DiskCache {
	parts := []string{"public", fmt.Sprintf("max-age=%d", config.HTTPMaxAgeSeconds)}
	if config.Immutable {
		parts = append(parts, "immutable")
	}
	return &DiskCache{
		config: config, root: config.Dir, active: config.VersionedDir,
		cacheControl: strings.Join(parts, ", "),
	}
}

func (c *DiskCache) CacheControl() string { return c.cacheControl }

func (c *DiskCache) Ensure() error { return os.MkdirAll(c.active, 0o755) }

func (c *DiskCache) Path(name, hexColor string, size int, format string) string {
	return filepath.Join(c.root, c.config.ResolvedNamespace, fmt.Sprintf("size-%d", size), name+"-"+strings.ToUpper(hexColor)+"."+format)
}

func (c *DiskCache) Read(name, hexColor string, size int, format string) ([]byte, error) {
	data, err := os.ReadFile(c.Path(name, hexColor, size, format))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *DiskCache) Write(name, hexColor string, size int, format string, data []byte) error {
	target := c.Path(name, hexColor, size, format)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.%d.%s.tmp", target, os.Getpid(), time.Now().UnixMilli(), hex.EncodeToString(random))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (c *DiskCache) Cleanup(reason string) (CleanupStats, bool, error) {
	c.cleanupMu.Lock()
	if c.cleanupRunning {
		c.cleanupMu.Unlock()
		return CleanupStats{}, false, nil
	}
	c.cleanupRunning = true
	c.cleanupMu.Unlock()
	defer func() { c.cleanupMu.Lock(); c.cleanupRunning = false; c.cleanupMu.Unlock() }()
	stats := CleanupStats{Reason: reason}
	if !c.config.Cleanup.Enabled {
		return stats, true, nil
	}
	if _, err := os.Stat(c.root); os.IsNotExist(err) {
		return stats, true, nil
	} else if err != nil {
		return stats, false, err
	}
	files, err := walkRegularFiles(c.root)
	if err != nil {
		return stats, false, err
	}
	now := time.Now()
	var pngs []cacheFile
	tmpAge := time.Duration(c.config.Cleanup.TmpMaxAgeMinutes) * time.Minute
	for _, name := range files {
		stats.Scanned++
		info, err := os.Stat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return stats, false, err
		}
		if strings.HasSuffix(name, ".tmp") {
			if now.Sub(info.ModTime()) >= tmpAge && removeFile(name) {
				stats.RemovedTmp++
			}
			continue
		}
		if !strings.HasSuffix(name, ".png") {
			continue
		}
		if c.config.Cleanup.MaxAgeDays != nil && now.Sub(info.ModTime()) >= time.Duration(*c.config.Cleanup.MaxAgeDays)*24*time.Hour {
			if removeFile(name) {
				stats.RemovedStale++
			}
			continue
		}
		pngs = append(pngs, cacheFile{path: name, size: info.Size(), mtime: info.ModTime()})
	}
	sort.SliceStable(pngs, func(i, j int) bool { return pngs[i].mtime.Before(pngs[j].mtime) })
	var totalBytes int64
	for _, file := range pngs {
		totalBytes += file.size
	}
	totalFiles := len(pngs)
	for _, file := range pngs {
		overFiles := c.config.Cleanup.MaxFiles != nil && totalFiles > *c.config.Cleanup.MaxFiles
		overBytes := c.config.Cleanup.MaxBytes != nil && totalBytes > int64(*c.config.Cleanup.MaxBytes)
		if !overFiles && !overBytes {
			break
		}
		if removeFile(file.path) {
			totalFiles--
			totalBytes -= file.size
			stats.RemovedOverLimit++
		}
	}
	stats.RemainingFiles = totalFiles
	if totalBytes > 0 {
		stats.RemainingBytes = totalBytes
	}
	if err := removeEmptyDirs(c.root); err != nil {
		return stats, false, err
	}
	return stats, true, nil
}

func (c *DiskCache) StartCleanupTimer(logf func(string, ...any), stop <-chan struct{}) *time.Ticker {
	cleanup := c.config.Cleanup
	if !cleanup.Enabled || cleanup.IntervalMS <= 0 {
		return nil
	}
	ticker := time.NewTicker(time.Duration(cleanup.IntervalMS) * time.Millisecond)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				stats, ok, err := c.Cleanup("scheduled")
				if err != nil {
					logf("[cdn] cache cleanup error: %v", err)
					continue
				}
				if !ok {
					continue
				}
				removed := stats.RemovedTmp + stats.RemovedStale + stats.RemovedOverLimit
				if removed > 0 {
					logf("[cdn] cache cleanup removed %d files (tmp=%d, stale=%d, over_limit=%d)", removed, stats.RemovedTmp, stats.RemovedStale, stats.RemovedOverLimit)
				}
			}
		}
	}()
	return ticker
}

func walkRegularFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.Type().IsRegular() {
			files = append(files, name)
		}
		return nil
	})
	return files, err
}

func removeFile(name string) bool {
	err := os.Remove(name)
	return err == nil || os.IsNotExist(err)
}

func removeEmptyDirs(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if dirs[i] == root {
			continue
		}
		if err := os.Remove(dirs[i]); err != nil && !os.IsNotExist(err) && !errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.ENOTDIR) {
			return err
		}
	}
	return nil
}
