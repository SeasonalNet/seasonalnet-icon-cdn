package cdn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRendererRejectsInvalidRequests(t *testing.T) {
	renderer, err := NewRenderer("")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		color  string
		size   int
		format string
		want   string
	}{
		{name: "bad_name", color: "FFFFFF", size: 64, format: "png", want: "invalid icon name"},
		{name: "siren", color: "GGGGGG", size: 64, format: "png", want: "invalid hex color"},
		{name: "siren", color: "FFFFFF", size: 0, format: "png", want: "size must be positive"},
		{name: "siren", color: "FFFFFF", size: 64, format: "gif", want: "unsupported image format"},
	}
	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			_, err := renderer.Render(test.name, test.color, test.size, test.format)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Render error = %v, want %q", err, test.want)
			}
		})
	}
	if data, err := renderer.Render("missing-icon", "FFFFFF", 64, "png"); err != nil || data != nil {
		t.Fatalf("unknown icon = %q, %v; want nil, nil", data, err)
	}
}

func TestNewRendererRejectsMissingAndEmptyDirectories(t *testing.T) {
	if _, err := NewRenderer(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing icons directory should fail")
	}
	if _, err := NewRenderer(t.TempDir()); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty icons directory error = %v", err)
	}
}

func TestRendererReportsMissingIconFileAfterInitialization(t *testing.T) {
	iconsDir := t.TempDir()
	iconPath := filepath.Join(iconsDir, "siren.svg")
	if err := os.WriteFile(iconPath, []byte(`<svg width="24" height="24"/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	renderer, err := NewRenderer(iconsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(iconPath); err != nil {
		t.Fatal(err)
	}
	if _, err := renderer.Render("siren", "FFFFFF", 64, "png"); err == nil || !strings.Contains(err.Error(), "read embedded icon") {
		t.Fatalf("missing icon read error = %v", err)
	}
}

func TestRendererReportsMalformedSVG(t *testing.T) {
	iconsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(iconsDir, "siren.svg"), []byte(`<svg><broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	renderer, err := NewRenderer(iconsDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := renderer.Render("siren", "FFFFFF", 64, "png"); err == nil || !strings.Contains(err.Error(), "render icon siren") || strings.Contains(err.Error(), "Stack:") {
		t.Fatalf("malformed SVG render error is not concise: %v", err)
	}
}

func TestSVGDimensionHelpersDoNotConfuseStrokeWidth(t *testing.T) {
	t.Run("raster sizing preserves existing dimensions", func(t *testing.T) {
		got := string(sizeSVG([]byte(`<svg width="24" height="24" stroke-width="2"></svg>`), 16))
		for _, expected := range []string{`width="24"`, `height="24"`, `stroke-width="2"`} {
			if !strings.Contains(got, expected) {
				t.Errorf("SVG %q missing %q", got, expected)
			}
		}
	})
	t.Run("SVG response dimensions do not replace stroke width", func(t *testing.T) {
		got := string(setSVGDimensions([]byte(`<svg stroke-width="2"></svg>`), 16))
		for _, expected := range []string{`width="16"`, `height="16"`, `stroke-width="2"`} {
			if !strings.Contains(got, expected) {
				t.Errorf("SVG %q missing %q", got, expected)
			}
		}
	})
	t.Run("no SVG root", func(t *testing.T) {
		if got := string(setSVGDimensions([]byte("text"), 16)); got != "text" {
			t.Fatalf("non-SVG input changed to %q", got)
		}
	})
}
