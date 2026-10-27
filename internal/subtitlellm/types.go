package subtitlellm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Version is the source bundle and mapping format version.
const Version = 1

// Subtitle layers.
const (
	LayerTop    = "top"
	LayerBottom = "bottom"
)

// Mapping group origins.
const (
	OriginModel    = "model"
	OriginRestored = "restored"
	// OriginTiming marks restored cues paired by time overlap after the model
	// did not place them.
	OriginTiming = "timing"
	// OriginTranslation marks direct pairs from a source and its translation.
	OriginTranslation = "translation"
)

// Mapping group kinds.
const (
	KindDialogue     = "dialogue"
	KindOnScreenText = "on_screen_text"
	KindUnknown      = "unknown"
)

// MaxDistanceMS is the maximum per-cue interval distance allowed between the
// two languages in one mapping group. It matches the prototype default.
const MaxDistanceMS = 8000

// schemaEnforcement describes the local validation guarantee recorded in a
// mapping. Server-side schema support is never assumed.
const schemaEnforcement = "unverified; strict local validation always applied"

// Cue is one source subtitle cue. Field names and semantics mirror the
// prototype sources.json so mappings and fingerprints remain interchangeable.
type Cue struct {
	ID            int         `json:"id"`
	Start         int         `json:"start"`
	End           int         `json:"end"`
	OriginalStart int         `json:"original_start"`
	OriginalEnd   int         `json:"original_end"`
	Raw           string      `json:"raw"`
	Plain         string      `json:"plain"`
	Display       string      `json:"display"`
	Style         string      `json:"style"`
	Alignment     int         `json:"alignment"`
	Position      *[2]float64 `json:"position"`
	Top           bool        `json:"top"`
	MultiSpeaker  bool        `json:"multi_speaker"`
	Changes       []string    `json:"changes"`
}

// Bundle is one immutable pair of source tracks. Fingerprint is the SHA-256 of
// the version and both cue lists in the prototype's canonical JSON form.
type Bundle struct {
	Version     int    `json:"version"`
	EN          []Cue  `json:"en"`
	ZH          []Cue  `json:"zh"`
	Fingerprint string `json:"fingerprint"`
	// ZHShiftMS is the offset LoadSources added to Chinese cue times to line
	// them up with English. OriginalStart/OriginalEnd keep the file times.
	ZHShiftMS int `json:"zh_shift_ms,omitempty"`
}

// Group is one bilingual alignment group. OwnerWindow is present in JSON for
// model groups and absent for restored one-sided groups, matching the
// prototype mapping layout.
type Group struct {
	ENIDs       []int  `json:"en_ids"`
	ZHIDs       []int  `json:"zh_ids"`
	Layer       string `json:"layer"`
	Origin      string `json:"origin"`
	Kind        string `json:"kind,omitempty"`
	OwnerWindow int    `json:"-"`

	ownerWindowSet bool
}

// groupJSON is the wire representation used to preserve owner_window presence.
type groupJSON struct {
	ENIDs       []int   `json:"en_ids"`
	ZHIDs       []int   `json:"zh_ids"`
	Layer       string  `json:"layer"`
	Origin      string  `json:"origin"`
	Kind        *string `json:"kind,omitempty"`
	OwnerWindow *int    `json:"owner_window,omitempty"`
}

// MarshalJSON writes owner_window for model groups and for groups decoded with
// an explicit owner_window key. Restored groups omit it, as in the prototype.
func (g Group) MarshalJSON() ([]byte, error) {
	out := groupJSON{
		ENIDs:  intsOrEmpty(g.ENIDs),
		ZHIDs:  intsOrEmpty(g.ZHIDs),
		Layer:  g.Layer,
		Origin: g.Origin,
	}
	if g.Kind != "" && g.Kind != KindUnknown {
		k := g.Kind
		out.Kind = &k
	}
	if g.Origin == OriginModel || g.ownerWindowSet {
		owner := g.OwnerWindow
		out.OwnerWindow = &owner
	}
	return json.Marshal(out)
}

// UnmarshalJSON records whether owner_window was present.
func (g *Group) UnmarshalJSON(data []byte) error {
	var raw groupJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	g.ENIDs = raw.ENIDs
	g.ZHIDs = raw.ZHIDs
	g.Layer = raw.Layer
	g.Origin = raw.Origin
	if raw.Kind != nil {
		g.Kind = *raw.Kind
	} else {
		g.Kind = ""
	}
	if raw.OwnerWindow != nil {
		g.OwnerWindow = *raw.OwnerWindow
		g.ownerWindowSet = true
	} else {
		g.OwnerWindow = 0
		g.ownerWindowSet = false
	}
	return nil
}

// MappingParameters records the settings that produced a mapping.
type MappingParameters struct {
	WindowMS          int    `json:"window_ms"`
	OverlapMS         int    `json:"overlap_ms"`
	MaxDistanceMS     int    `json:"max_distance_ms"`
	Model             string `json:"model"`
	Endpoint          string `json:"endpoint"`
	ResponseFormat    string `json:"response_format"`
	SchemaEnforcement string `json:"schema_enforcement"`
}

// Mapping is the ID-only alignment result. Subtitle text is never model output;
// it always comes from the source bundle.
type Mapping struct {
	Version           int               `json:"version"`
	SourceFingerprint string            `json:"source_fingerprint"`
	Parameters        MappingParameters `json:"parameters"`
	Groups            []Group           `json:"groups"`
	SplitChecks       []SplitCheck      `json:"split_checks,omitempty"`
	Fingerprint       string            `json:"fingerprint,omitempty"`
}

// SplitCheck records whether showing ZH[i] together with EN[i] keeps every
// Chinese part with the English part it translates. The renderer splits a
// pair part by part only when a check says Same.
type SplitCheck struct {
	ZH   []string `json:"zh"`
	EN   []string `json:"en"`
	Same bool     `json:"same"`
}

// SplitCheckAudit counts the order checks of one alignment run.
type SplitCheckAudit struct {
	Asked      int      `json:"asked"`
	Confirmed  int      `json:"confirmed"`
	Unanswered int      `json:"unanswered"`
	Failures   []string `json:"failures,omitempty"`
}

// Coverage reports how many source cues each language contributed.
type Coverage struct {
	EN int `json:"en"`
	ZH int `json:"zh"`
}

// Provenance reports which model produced a mapping and whether an explicitly
// imported prototype mapping was requested with a different model setting.
type Provenance struct {
	Source            string `json:"source"`
	MappingPath       string `json:"mapping_path,omitempty"`
	SourcesPath       string `json:"sources_path,omitempty"`
	MappingModel      string `json:"mapping_model,omitempty"`
	ConfiguredModel   string `json:"configured_model,omitempty"`
	ModelMismatch     bool   `json:"model_mismatch,omitempty"`
	SourceFingerprint string `json:"source_fingerprint"`
}

// RejectedRow records one model row that local validation refused. Row is nil
// for rejections that apply to an entire row group after duplicate detection.
type RejectedRow struct {
	Window int             `json:"window"`
	Row    *int            `json:"row,omitempty"`
	Reason string          `json:"reason"`
	Value  json.RawMessage `json:"value,omitempty"`
}

// WindowMissing records cue IDs not covered by any valid row in a window.
type WindowMissing struct {
	Window int   `json:"window"`
	EN     []int `json:"en"`
	ZH     []int `json:"zh"`
}

// CacheCorruption records one cache entry discarded as corrupt and re-requested.
// Path and Reason never contain credentials.
type CacheCorruption struct {
	Window int    `json:"window"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// RequestRetry records a failed HTTP/model attempt that was retried. Reason is
// redacted and never contains a response body.
type RequestRetry struct {
	Window int    `json:"window"`
	Reason string `json:"reason"`
}

// WindowFailure records the sanitized failure reason for a sliding window whose
// attempts were exhausted or non-retryable.
type WindowFailure struct {
	Window int    `json:"window"`
	Reason string `json:"reason"`
}

// Audit is the deterministic validation record for an alignment run.
type Audit struct {
	Rejected          []RejectedRow     `json:"rejected"`
	Conflicts         []Group           `json:"conflicts"`
	ContextDuplicates int               `json:"context_duplicates"`
	WindowMissing     []WindowMissing   `json:"window_missing"`
	Restored          map[string][]int  `json:"restored"`
	TimingPaired      map[string][]int  `json:"timing_paired,omitempty"`
	Pairing           Pairing           `json:"pairing"`
	CacheCorrupted    []CacheCorruption `json:"cache_corrupted,omitempty"`
	RequestRetries    []RequestRetry    `json:"request_retries,omitempty"`
	FailedWindows     []int             `json:"failed_windows,omitempty"`
	WindowFailures    []WindowFailure   `json:"window_failures,omitempty"`
	CacheHits         int               `json:"cache_hits,omitempty"`
	HTTPAttempts      int               `json:"http_attempts_this_run,omitempty"`
	ResolvedModels    []string          `json:"resolved_models,omitempty"`
	SplitChecks       *SplitCheckAudit  `json:"split_checks,omitempty"`
	Coverage          Coverage          `json:"coverage"`
	Provenance        *Provenance       `json:"provenance,omitempty"`
}

// intsOrEmpty returns a non-nil slice so JSON output uses [] rather than null.
func intsOrEmpty(ids []int) []int {
	if ids == nil {
		return []int{}
	}
	return ids
}

// bundleDigest computes the prototype-compatible source bundle fingerprint.
func bundleDigest(b Bundle) (string, error) {
	version := b.Version
	if version == 0 {
		version = Version
	}
	return digestValue(map[string]any{
		"version": version,
		"en":      cuesValue(b.EN),
		"zh":      cuesValue(b.ZH),
	})
}

// signature identifies a model group for context-duplicate detection.
func signature(group Group) string {
	data, err := json.Marshal([]any{intsValue(group.ENIDs), intsValue(group.ZHIDs), group.Layer})
	if err != nil {
		return ""
	}
	return string(data)
}

func cuesValue(cues []Cue) []any {
	out := make([]any, len(cues))
	for i, cue := range cues {
		out[i] = cueValue(cue)
	}
	return out
}

func cueValue(c Cue) map[string]any {
	var position any
	if c.Position != nil {
		position = []any{c.Position[0], c.Position[1]}
	}
	changes := make([]any, len(c.Changes))
	for i, change := range c.Changes {
		changes[i] = change
	}
	return map[string]any{
		"id":             c.ID,
		"start":          c.Start,
		"end":            c.End,
		"original_start": c.OriginalStart,
		"original_end":   c.OriginalEnd,
		"raw":            c.Raw,
		"plain":          c.Plain,
		"display":        c.Display,
		"style":          c.Style,
		"alignment":      c.Alignment,
		"position":       position,
		"top":            c.Top,
		"multi_speaker":  c.MultiSpeaker,
		"changes":        changes,
	}
}

// mappingDigest computes the prototype-compatible mapping fingerprint over all
// fields except fingerprint itself.
func mappingDigest(m Mapping) (string, error) {
	groups := make([]any, len(m.Groups))
	for i, group := range m.Groups {
		groups[i] = groupValue(group)
	}
	value := map[string]any{
		"version":            m.Version,
		"source_fingerprint": m.SourceFingerprint,
		"parameters": map[string]any{
			"window_ms":          m.Parameters.WindowMS,
			"overlap_ms":         m.Parameters.OverlapMS,
			"max_distance_ms":    m.Parameters.MaxDistanceMS,
			"model":              m.Parameters.Model,
			"endpoint":           m.Parameters.Endpoint,
			"response_format":    m.Parameters.ResponseFormat,
			"schema_enforcement": m.Parameters.SchemaEnforcement,
		},
		"groups": groups,
	}
	// Older mappings have no checks; leaving the key out keeps their fingerprints valid.
	if len(m.SplitChecks) > 0 {
		checks := make([]any, len(m.SplitChecks))
		for i, check := range m.SplitChecks {
			checks[i] = map[string]any{"zh": stringsValue(check.ZH), "en": stringsValue(check.EN), "same": check.Same}
		}
		value["split_checks"] = checks
	}
	return digestValue(value)
}

func stringsValue(values []string) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

func groupValue(g Group) map[string]any {
	en := intsValue(g.ENIDs)
	zh := intsValue(g.ZHIDs)
	out := map[string]any{
		"en_ids": en,
		"zh_ids": zh,
		"layer":  g.Layer,
		"origin": g.Origin,
	}
	if g.Origin == OriginModel || g.ownerWindowSet {
		out["owner_window"] = g.OwnerWindow
	}
	if g.Kind != "" && g.Kind != KindUnknown {
		out["kind"] = g.Kind
	}
	return out
}

func intsValue(ids []int) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// rejectDuplicateJSONKeys rejects objects that contain the same key twice at any
// nesting level. encoding/json silently keeps the last value, which would let a
// malformed model response, mapping, or cache record change meaning.
func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	type frame struct {
		object       bool
		expectingKey bool
		keys         map[string]bool
	}
	var stack []*frame
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{', '[':
				if len(stack) > 0 && stack[len(stack)-1].object {
					// A container was consumed as an object value.
					stack[len(stack)-1].expectingKey = true
				}
				stack = append(stack, &frame{object: delim == '{'})
				if delim == '{' {
					stack[len(stack)-1].expectingKey = true
					stack[len(stack)-1].keys = map[string]bool{}
				}
			case '}', ']':
				if len(stack) == 0 {
					return fmt.Errorf("unexpected %q", delim)
				}
				stack = stack[:len(stack)-1]
				if len(stack) > 0 && stack[len(stack)-1].object {
					stack[len(stack)-1].expectingKey = true
				}
			}
			continue
		}
		if len(stack) == 0 {
			continue
		}
		top := stack[len(stack)-1]
		if !top.object {
			continue
		}
		if top.expectingKey {
			key, ok := token.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if top.keys[key] {
				return fmt.Errorf("duplicate object key %q", key)
			}
			top.keys[key] = true
			top.expectingKey = false
			continue
		}
		// A scalar value finished; the next object token is a key.
		top.expectingKey = true
	}
}

// digestValue serializes a JSON-like value exactly like Python's
// json.dumps(value, ensure_ascii=False, sort_keys=True) and returns its SHA-256.
func digestValue(value any) (string, error) {
	var b strings.Builder
	if err := writeCanonical(&b, value); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), nil
}

func writeCanonical(b *strings.Builder, value any) error {
	switch v := value.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int:
		b.WriteString(strconv.Itoa(v))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case float64:
		b.WriteString(pythonFloatRepr(v))
	case string:
		writePythonString(b, v)
	case []any:
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := writeCanonical(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			writePythonString(b, key)
			b.WriteString(": ")
			if err := writeCanonical(b, v[key]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON value %T", value)
	}
	return nil
}

// writePythonString escapes a string the way Python json.dumps does with
// ensure_ascii=False: only quotes, backslashes, and control characters.
func writePythonString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// pythonFloatRepr formats a float like Python's repr, including the trailing
// ".0" for integral values and Python's scientific-notation thresholds.
func pythonFloatRepr(value float64) string {
	if math.IsNaN(value) {
		return "NaN"
	}
	if math.IsInf(value, 1) {
		return "Infinity"
	}
	if math.IsInf(value, -1) {
		return "-Infinity"
	}
	sign := ""
	if math.Signbit(value) {
		sign = "-"
		value = -value
	}
	if value == 0 {
		return sign + "0.0"
	}

	// Shortest round-trip digits in scientific form.
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	mantissa, exponentText, found := strings.Cut(scientific, "e")
	if !found {
		return sign + scientific
	}
	exponent, err := strconv.Atoi(exponentText)
	if err != nil {
		return sign + scientific
	}
	digits := strings.Replace(mantissa, ".", "", 1)

	if exponent >= -4 && exponent < 16 {
		switch {
		case exponent < 0:
			return sign + "0." + strings.Repeat("0", -exponent-1) + digits
		case len(digits) <= exponent+1:
			return sign + digits + strings.Repeat("0", exponent+1-len(digits)) + ".0"
		default:
			return sign + digits[:exponent+1] + "." + digits[exponent+1:]
		}
	}

	mantissaOut := digits[:1]
	if len(digits) > 1 {
		mantissaOut += "." + digits[1:]
	}
	exponentSign := "+"
	if exponent < 0 {
		exponentSign = "-"
		exponent = -exponent
	}
	exponentDigits := strconv.Itoa(exponent)
	if len(exponentDigits) < 2 {
		exponentDigits = "0" + exponentDigits
	}
	return sign + mantissaOut + "e" + exponentSign + exponentDigits
}
