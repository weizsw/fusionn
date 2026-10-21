package subtitlellm

import (
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

var (
	fontCacheMu  sync.RWMutex
	fontCache    = make(map[string]*sfnt.Font)
	fontNotFound = make(map[string]bool)

	// Mock or override hook for deterministic testing.
	fontWidthMeasurerOverride func(text string, fontName string, fontSize float64, scaleX int, spacing float64) (float64, bool)
)

// MeasureTextWidth calculates the exact pixel width of text rendered with the
// specified font, size, horizontal scale and spacing. If the font cannot be found
// or verified, it returns (0, false) so callers can safely fall back.
func MeasureTextWidth(text string, fontName string, fontSize float64, scaleX int, spacing float64, fontsDir string) (float64, bool) {
	if fontWidthMeasurerOverride != nil {
		return fontWidthMeasurerOverride(text, fontName, fontSize, scaleX, spacing)
	}
	if strings.TrimSpace(text) == "" {
		return 0, true
	}
	f := findFont(fontName, fontsDir)
	if f == nil {
		return 0, false
	}
	var b sfnt.Buffer
	var totalAdvance float64
	for _, r := range text {
		glyphIndex, err := f.GlyphIndex(&b, r)
		if err != nil || glyphIndex == 0 {
			// If a glyph is missing from the font, metrics are incomplete.
			// Return false to safely fallback rather than guessing.
			return 0, false
		}
		adv, err := f.GlyphAdvance(&b, glyphIndex, fixed.I(int(math.Round(fontSize))), font.HintingNone)
		if err != nil {
			return 0, false
		}
		totalAdvance += float64(adv) / 64.0
	}
	if scaleX <= 0 {
		scaleX = 100
	}
	width := totalAdvance * (float64(scaleX) / 100.0)
	runes := []rune(text)
	if len(runes) > 1 && spacing != 0 {
		width += float64(len(runes)-1) * spacing
	}
	return width, true
}

func normalizeFontName(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func findFont(fontName, fontsDir string) *sfnt.Font {
	normalized := normalizeFontName(fontName)
	if normalized == "" {
		return nil
	}

	fontCacheMu.RLock()
	if f, ok := fontCache[normalized]; ok {
		fontCacheMu.RUnlock()
		return f
	}
	if fontNotFound[normalized] {
		fontCacheMu.RUnlock()
		return nil
	}
	fontCacheMu.RUnlock()

	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()

	// Double check after lock
	if f, ok := fontCache[normalized]; ok {
		return f
	}
	if fontNotFound[normalized] {
		return nil
	}

	var dirs []string
	if fontsDir != "" {
		dirs = append(dirs, fontsDir)
	}
	// Common system font paths across macOS and Linux
	dirs = append(dirs,
		"/Library/Fonts",
		"/System/Library/Fonts",
		"/System/Library/Fonts/Supplemental",
		"/usr/share/fonts",
		"/usr/local/share/fonts",
		"/data/fonts",
		"/app/fonts",
	)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs,
			filepath.Join(home, "Library/Fonts"),
			filepath.Join(home, ".fonts"),
			filepath.Join(home, ".local/share/fonts"),
		)
	}

	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".ttf" && ext != ".otf" && ext != ".ttc" {
				return nil
			}
			// Fast check: if filename matches normalized font name
			base := normalizeFontName(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
			if !strings.Contains(base, normalized) && !strings.Contains(normalized, base) {
				// Don't read file if base name doesn't even share substring
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if ext == ".ttc" {
				coll, err := sfnt.ParseCollection(data)
				if err == nil {
					for i := 0; i < coll.NumFonts(); i++ {
						parsed, err := coll.Font(i)
						if err == nil && fontMatches(parsed, normalized) {
							fontCache[normalized] = parsed
							return fs.SkipAll
						}
					}
				}
				return nil
			}
			parsed, err := sfnt.Parse(data)
			if err == nil && fontMatches(parsed, normalized) {
				fontCache[normalized] = parsed
				return fs.SkipAll
			}
			return nil
		})
		if fontCache[normalized] != nil {
			return fontCache[normalized]
		}
	}

	fontNotFound[normalized] = true
	return nil
}

func fontMatches(f *sfnt.Font, normalizedTarget string) bool {
	var b sfnt.Buffer
	for _, id := range []sfnt.NameID{sfnt.NameIDFamily, sfnt.NameIDFull, sfnt.NameIDPostScript, sfnt.NameIDTypographicFamily} {
		name, err := f.Name(&b, id)
		if err == nil && normalizeFontName(name) == normalizedTarget {
			return true
		}
	}
	return false
}
