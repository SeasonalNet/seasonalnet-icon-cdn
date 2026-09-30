package cdn

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/davidbyttow/govips/v2/vips"
)

//go:embed assets/lucide/VERSION assets/lucide/icons/*.svg
var lucideAssets embed.FS

var (
	iconNamePattern = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	hexColorPattern = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)
	currentColorRE  = regexp.MustCompile(`(?i)currentColor`)
	widthRE         = regexp.MustCompile(`(^|[[:space:]])width="[^"]*"`)
	heightRE        = regexp.MustCompile(`(^|[[:space:]])height="[^"]*"`)
	imageEngineOnce sync.Once
	imageEngineErr  error
)

type Renderer struct {
	icons    map[string]struct{}
	version  string
	iconsDir string
}

func NewRenderer(iconsDir string) (*Renderer, error) {
	var entries []fs.DirEntry
	var err error
	if iconsDir == "" {
		entries, err = fs.ReadDir(lucideAssets, "assets/lucide/icons")
	} else {
		entries, err = os.ReadDir(iconsDir)
	}
	if err != nil {
		return nil, fmt.Errorf("read embedded Lucide assets: %w", err)
	}
	icons := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".svg") {
			continue
		}
		icons[strings.TrimSuffix(entry.Name(), ".svg")] = struct{}{}
	}
	if len(icons) == 0 {
		return nil, fmt.Errorf("embedded Lucide icon set is empty")
	}
	if err := initializeImageEngine(); err != nil {
		return nil, fmt.Errorf("initialize libvips: %w", err)
	}
	versionData, err := lucideAssets.ReadFile("assets/lucide/VERSION")
	if err != nil {
		return nil, fmt.Errorf("read embedded Lucide version: %w", err)
	}
	return &Renderer{icons: icons, version: strings.TrimSpace(string(versionData)), iconsDir: iconsDir}, nil
}

func initializeImageEngine() error {
	imageEngineOnce.Do(func() {
		vips.LoggingSettings(nil, vips.LogLevelWarning)
		imageEngineErr = vips.Startup(nil)
	})
	return imageEngineErr
}

// ShutdownImageEngine releases the process-wide libvips runtime.
func ShutdownImageEngine() { vips.Shutdown() }

// Close is retained so callers can use the same lifecycle as the original
// renderer. Per-image memory is released after each Render; libvips itself is
// process-wide and is shut down with ShutdownImageEngine.
func (r *Renderer) Close() error { return nil }

func (r *Renderer) Version() string { return r.version }

func embeddedLucideVersion() string {
	data, err := lucideAssets.ReadFile("assets/lucide/VERSION")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

func (r *Renderer) IconNames() []string {
	names := make([]string, 0, len(r.icons))
	for name := range r.icons {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Renderer) HasIcon(name string) bool {
	_, ok := r.icons[name]
	return ok
}

func (r *Renderer) Render(name, hex string, size int, format string) ([]byte, error) {
	if !iconNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid icon name")
	}
	if !hexColorPattern.MatchString(hex) {
		return nil, fmt.Errorf("invalid hex color")
	}
	if size < 1 {
		return nil, fmt.Errorf("size must be positive")
	}
	if format != "png" && format != "svg" {
		return nil, fmt.Errorf("unsupported image format %q", format)
	}
	if !r.HasIcon(name) {
		return nil, nil
	}

	var svg []byte
	var err error
	if r.iconsDir == "" {
		svg, err = lucideAssets.ReadFile(path.Join("assets/lucide/icons", name+".svg"))
	} else {
		svg, err = os.ReadFile(path.Join(r.iconsDir, name+".svg"))
	}
	if err != nil {
		return nil, fmt.Errorf("read embedded icon %s: %w", name, err)
	}
	hex = strings.ToUpper(strings.TrimPrefix(hex, "#"))
	colored := currentColorRE.ReplaceAll(svg, []byte("#"+hex))
	colored = sizeSVG(colored, size)
	if format == "svg" {
		return setSVGDimensions(colored, size), nil
	}

	importParams := vips.NewImportParams()
	importParams.Density.Set(72)
	image, err := vips.LoadThumbnailFromBuffer(colored, size, size, vips.InterestingCentre, vips.SizeBoth, importParams)
	if err != nil {
		return nil, fmt.Errorf("render icon %s: %s", name, conciseVipsError(err))
	}
	defer image.Close()
	pngParams := vips.NewPngExportParams()
	pngParams.StripMetadata = true
	png, _, err := image.ExportPng(pngParams)
	if err != nil {
		return nil, fmt.Errorf("encode icon %s: %s", name, conciseVipsError(err))
	}
	return png, nil
}

func conciseVipsError(err error) string {
	message, _, _ := strings.Cut(err.Error(), "\nStack:\n")
	return message
}

func sizeSVG(svg []byte, size int) []byte {
	root := []byte("<svg")
	if !bytes.Contains(svg, root) {
		return svg
	}
	if widthRE.Match(svg) {
		return svg
	}
	attributes := []byte(fmt.Sprintf(` width="%d" height="%d"`, size, size))
	return bytes.Replace(svg, root, append(root, attributes...), 1)
}

func setSVGDimensions(svg []byte, size int) []byte {
	return setSVGDimension(setSVGDimension(svg, "width", size), "height", size)
}

func setSVGDimension(svg []byte, attribute string, size int) []byte {
	root := []byte("<svg")
	if !bytes.Contains(svg, root) {
		return svg
	}
	pattern := widthRE
	if attribute == "height" {
		pattern = heightRE
	}
	if pattern.Match(svg) {
		return pattern.ReplaceAllFunc(svg, func(match []byte) []byte {
			prefix := match[:1]
			return append(append([]byte(nil), prefix...), []byte(fmt.Sprintf(`%s="%d"`, attribute, size))...)
		})
	}
	return bytes.Replace(svg, root, append(root, []byte(fmt.Sprintf(` %s="%d"`, attribute, size))...), 1)
}
