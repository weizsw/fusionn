package subtitlellm

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultPlayResX = 1920.0
	defaultPlayResY = 1080.0
)

// LoadSources parses native SRT or ASS English and Chinese subtitle files into a
// fingerprinted source bundle. Parsing mirrors the prototype: raw text is kept
// verbatim, plain strips override blocks and HTML-style tags, display flattens
// presentation newlines, and native ASS alignment/position determine Top.
func LoadSources(enPath, zhPath string) (Bundle, error) {
	en, err := parseTrack(enPath)
	if err != nil {
		return Bundle{}, fmt.Errorf("parse English subtitles %q: %w", enPath, err)
	}
	zh, err := parseTrack(zhPath)
	if err != nil {
		return Bundle{}, fmt.Errorf("parse Chinese subtitles %q: %w", zhPath, err)
	}
	// English comes from the video and is the timing reference; a Chinese
	// track from another release may be offset by a constant amount.
	shift := estimateZHShift(en, zh)
	if shift != 0 {
		zh = shiftCues(zh, shift)
	}
	bundle := Bundle{Version: Version, EN: en, ZH: zh, ZHShiftMS: shift}
	fingerprint, err := bundleDigest(bundle)
	if err != nil {
		return Bundle{}, err
	}
	bundle.Fingerprint = fingerprint
	return bundle, nil
}

type rawCue struct {
	start int
	end   int
	text  string
	style string
}

type styleInfo struct {
	alignment int
}

// parseTrack parses one subtitle file, including its native style table.
func parseTrack(path string) ([]Cue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	var cues []rawCue
	var styles map[string]styleInfo
	playResX, playResY := defaultPlayResX, defaultPlayResY
	if strings.Contains(content, "[Script Info]") || strings.Contains(content, "[V4+ Styles]") || strings.Contains(content, "[V4 Styles]") {
		cues, styles, playResX, playResY, err = parseASS(content)
	} else {
		cues, err = parseSRT(content)
		styles = map[string]styleInfo{}
	}
	if err != nil {
		return nil, err
	}
	if len(cues) == 0 {
		return nil, fmt.Errorf("no subtitle cues found")
	}

	result := make([]Cue, 0, len(cues))
	for i, cue := range cues {
		if cue.start < 0 || cue.end <= cue.start {
			return nil, fmt.Errorf("invalid source time for %s cue %d (%d-%d)", filepath.Base(path), i+1, cue.start, cue.end)
		}
		style, ok := styles[cue.style]
		if !ok {
			// Unknown or absent styles use the pysubs2 default (bottom center).
			style = styleInfo{alignment: 2}
		}
		result = append(result, buildCue(i+1, cue, style, playResX, playResY))
	}
	return result, nil
}

func parseSRT(content string) ([]rawCue, error) {
	// Scan line by line like pysubs2: a line with two timestamps starts a cue
	// and every following non-timestamp line belongs to its text. This keeps
	// blank lines inside cue text and tolerates missing cue numbers.
	var cues []rawCue
	var current *rawCue
	var textLines []string
	flush := func() {
		if current == nil {
			return
		}
		text := strings.Join(textLines, "\n")
		// pysubs2 first strips a trailing blank line plus the next cue number,
		// then strips the remaining text, which in Python trims Unicode spaces.
		if len(textLines) >= 2 {
			blankPrefix := true
			for _, line := range textLines[:len(textLines)-1] {
				if strings.TrimSpace(line) != "" {
					blankPrefix = false
					break
				}
			}
			if blankPrefix && srtCueNumber.MatchString(strings.TrimSpace(textLines[len(textLines)-1])) {
				text = ""
			} else {
				text = srtTrailingCueNumber.ReplaceAllString(text, "")
			}
		}
		text = strings.TrimFunc(text, unicode.IsSpace)
		cues = append(cues, rawCue{start: current.start, end: current.end, text: prepareSRTText(text), style: "Default"})
		textLines = nil
	}
	for _, line := range strings.Split(content, "\n") {
		stamps := srtTimestampLine.FindAllString(line, -1)
		if len(stamps) == 2 {
			flush()
			start, err := parseSRTTime(stamps[0])
			if err != nil {
				return nil, fmt.Errorf("SRT cue %d: %w", len(cues)+1, err)
			}
			end, err := parseSRTTime(stamps[1])
			if err != nil {
				return nil, fmt.Errorf("SRT cue %d: %w", len(cues)+1, err)
			}
			current = &rawCue{start: start, end: end}
			continue
		}
		if current != nil {
			textLines = append(textLines, line)
		}
	}
	flush()
	return cues, nil
}

// prepareSRTText mirrors pysubs2's SRT text normalization: newlines become \N,
// four supported HTML tags become inline override blocks, and other tags are
// stripped (case-insensitively, with ASCII-only space matching).
func prepareSRTText(text string) string {
	text = strings.ReplaceAll(text, "\n", `\N`)
	for _, tag := range []struct {
		pattern *regexp.Regexp
		block   string
	}{
		{srtITag, `{\i1}`}, {srtITagClose, `{\i0}`},
		{srtSTag, `{\s1}`}, {srtSTagClose, `{\s0}`},
		{srtUTag, `{\u1}`}, {srtUTagClose, `{\u0}`},
		{srtBTag, `{\b1}`}, {srtBTagClose, `{\b0}`},
	} {
		text = tag.pattern.ReplaceAllLiteralString(text, tag.block)
	}
	return srtOtherTag.ReplaceAllString(text, "")
}

var (
	srtTimestampLine     = regexp.MustCompile(`(\d{1,2}):(\d{1,2}):(\d{1,2})[.,](\d{1,3})`)
	srtCueNumber         = regexp.MustCompile(`^\d+$`)
	srtTrailingCueNumber = regexp.MustCompile(`\n+ *\d+ *$`)
	srtITag              = regexp.MustCompile(`(?i)< *i *>`)
	srtITagClose         = regexp.MustCompile(`(?i)< */ *i *>`)
	srtSTag              = regexp.MustCompile(`(?i)< *s *>`)
	srtSTagClose         = regexp.MustCompile(`(?i)< */ *s *>`)
	srtUTag              = regexp.MustCompile(`(?i)< *u *>`)
	srtUTagClose         = regexp.MustCompile(`(?i)< */ *u *>`)
	srtBTag              = regexp.MustCompile(`(?i)< *b *>`)
	srtBTagClose         = regexp.MustCompile(`(?i)< */ *b *>`)
	srtOtherTag          = regexp.MustCompile(`(?i)< */? *[a-zA-Z][^>]*>`)
)

func parseSRTTime(text string) (int, error) {
	match := srtTimestamp.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return 0, fmt.Errorf("invalid timestamp %q", text)
	}
	hours := 0
	if match[1] != "" {
		hours, _ = strconv.Atoi(match[1])
	}
	minutes, _ := strconv.Atoi(match[2])
	seconds, _ := strconv.Atoi(match[3])
	fraction := match[4]
	millis, _ := strconv.Atoi(fraction)
	for i := len(fraction); i < 3; i++ {
		millis *= 10
	}
	return ((hours*60+minutes)*60+seconds)*1000 + millis, nil
}

var (
	srtTimestamp = regexp.MustCompile(`^(?:(\d+):)?(\d{1,2}):(\d{2})[.,](\d{1,3})$`)
	assTimestamp = regexp.MustCompile(`^(-?)(?:(\d+):)?(\d{1,2}):(\d{2})(?:[.,](\d{1,3}))?$`)
)

// parseASSTime parses H:MM:SS.cc or H:MM:SS.mmm; a bare H:MM:SS is accepted.
func parseASSTime(text string) (int, error) {
	match := assTimestamp.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return 0, fmt.Errorf("invalid timestamp %q", text)
	}
	hours, _ := strconv.Atoi(match[2])
	minutes, _ := strconv.Atoi(match[3])
	seconds, _ := strconv.Atoi(match[4])
	fraction := match[5]
	millis := 0
	if fraction != "" {
		millis, _ = strconv.Atoi(fraction)
		for i := len(fraction); i < 3; i++ {
			millis *= 10
		}
	}
	millis += ((hours*60+minutes)*60 + seconds) * 1000
	if match[1] == "-" {
		millis = -millis
	}
	return millis, nil
}

var (
	assSection = regexp.MustCompile(`^.{0,3}\[[^]]*[a-z][^]]*\]$`)
	assKV      = regexp.MustCompile(`^([^:]+):(.*)$`)
)

// parseASS extracts dialogue cues, the style alignment table, and PlayRes.
func parseASS(content string) ([]rawCue, map[string]styleInfo, float64, float64, error) {
	styles := make(map[string]styleInfo)
	var cues []rawCue
	playResX, playResY := defaultPlayResX, defaultPlayResY
	section := ""

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if assSection.MatchString(line) {
			section = strings.Trim(line, "[]")
			continue
		}
		kvs := assKV.FindStringSubmatch(line)
		if kvs == nil {
			continue
		}
		key := strings.TrimSpace(kvs[1])
		value := strings.TrimSpace(kvs[2])
		switch {
		case strings.Contains(section, "Script Info"):
			switch strings.ToLower(key) {
			case "playresx":
				if parsed, err := strconv.ParseFloat(value, 64); err == nil {
					playResX = parsed
				}
			case "playresy":
				if parsed, err := strconv.ParseFloat(value, 64); err == nil {
					playResY = parsed
				}
			}
		case strings.Contains(section, "Styles") && key == "Style":
			fields := splitASSFields(value)
			style := styleInfo{alignment: 2}
			if strings.Contains(section, "V4+") {
				// Name, Fontname, Fontsize, ... Shadow, Alignment.
				if len(fields) >= 19 {
					if parsed, err := strconv.Atoi(strings.TrimSpace(fields[18])); err == nil {
						style.alignment = parsed
					}
				}
			} else if len(fields) >= 13 {
				// Legacy SSA alignment (1-11) uses a different numbering.
				if parsed, err := strconv.Atoi(strings.TrimSpace(fields[12])); err == nil {
					style.alignment = ssaToASSAlignment(parsed)
				}
			}
			styles[strings.TrimSpace(fields[0])] = style
		case strings.Contains(section, "Events") && (key == "Dialogue" || key == "Comment"):
			if key == "Comment" {
				continue
			}
			fields := splitASSFieldsN(value, 10)
			if len(fields) < 10 {
				return nil, nil, 0, 0, fmt.Errorf("malformed ASS dialogue %q", line)
			}
			start, err := parseASSTime(fields[1])
			if err != nil {
				return nil, nil, 0, 0, err
			}
			end, err := parseASSTime(fields[2])
			if err != nil {
				return nil, nil, 0, 0, err
			}
			cues = append(cues, rawCue{
				start: start,
				end:   end,
				text:  fields[9],
				style: strings.TrimSpace(fields[3]),
			})
		}
	}
	return cues, styles, playResX, playResY, nil
}

func splitASSFields(text string) []string {
	return strings.Split(text, ",")
}

// splitASSFieldsN splits on the first n-1 commas so the text field may contain
// commas, matching pysubs2's parser.
func splitASSFieldsN(text string, n int) []string {
	if n <= 0 {
		return nil
	}
	return strings.SplitN(text, ",", n)
}

// ssaToASSAlignment converts legacy SSA alignment to ASS numpad alignment.
func ssaToASSAlignment(value int) int {
	switch value {
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 3
	case 5:
		return 7
	case 6:
		return 8
	case 7:
		return 9
	case 9:
		return 4
	case 10:
		return 5
	case 11:
		return 6
	default:
		return 2
	}
}

var (
	assHTMLBlock     = regexp.MustCompile(`(?i)</?(?:i|b|u|s|font)(?:\s[^>]*)?>`)
	assOverrideBlock = regexp.MustCompile(`\{[^}]*\}`)
	assNativeAlign   = regexp.MustCompile(`\\an([1-9])`)
	assLegacyAlign   = regexp.MustCompile(`\\a(1[01]|[1-9])(?:\D|$)`)
	assPosition      = regexp.MustCompile(`\\pos\(\s*([\d.]+)\s*,\s*([\d.]+)\s*\)`)

	// Python's \s matches Unicode whitespace (including U+3000), unlike Go's.
	speakerLine = regexp.MustCompile(`^[\t\n\v\f\r \x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]*[-–—]`)
	hanText     = regexp.MustCompile(`[\x{3400}-\x{9fff}]`)
)

// buildCue derives plain/display/native placement from a raw cue exactly like
// the prototype read_track function.
// speakerMarks are the dashes that open a speaker's line.
const speakerMarks = "-－–—"

// cleanLine is the subtitle cleanup applied before alignment. Any run of
// spaces (including full-width spaces) becomes one half-width space, and a
// leading speaker dash becomes "- ". Other characters stay unchanged.
func cleanLine(line string) string {
	line = strings.Join(strings.Fields(line), " ")
	mark, size := utf8.DecodeRuneInString(line)
	if line == "" || !strings.ContainsRune(speakerMarks, mark) {
		return line
	}
	rest := strings.TrimPrefix(line[size:], " ")
	next, _ := utf8.DecodeRuneInString(rest)
	// "--", "——" and "-5" are not speaker marks.
	if rest == "" || strings.ContainsRune(speakerMarks, next) || unicode.IsDigit(next) {
		return line
	}
	return "- " + rest
}

func buildCue(id int, cue rawCue, style styleInfo, playResX, playResY float64) Cue {
	alignment := style.alignment
	native := assNativeAlign.FindAllStringSubmatch(cue.text, -1)
	if len(native) > 0 {
		alignment, _ = strconv.Atoi(native[len(native)-1][1])
	}
	legacy := assLegacyAlign.FindAllStringSubmatch(cue.text, -1)
	if len(legacy) > 0 && len(native) == 0 {
		value, _ := strconv.Atoi(legacy[len(legacy)-1][1])
		alignment = ssaToASSAlignment(value)
	}

	var position *[2]float64
	if match := assPosition.FindStringSubmatch(cue.text); match != nil {
		x, _ := strconv.ParseFloat(match[1], 64)
		y, _ := strconv.ParseFloat(match[2], 64)
		if playResX <= 0 {
			playResX = defaultPlayResX
		}
		if playResY <= 0 {
			playResY = defaultPlayResY
		}
		position = &[2]float64{x * defaultPlayResX / playResX, y * defaultPlayResY / playResY}
	}

	// Keep raw text independently. Only format markup and whitespace change.
	plain := assHTMLBlock.ReplaceAllString(cue.text, "")
	plain = assOverrideBlock.ReplaceAllString(plain, "")
	plain = strings.ReplaceAll(plain, `\h`, " ")
	plain = strings.ReplaceAll(plain, `\n`, "\n")
	plain = strings.ReplaceAll(plain, `\N`, "\n")
	plain = html.UnescapeString(plain)
	markupChanged := cue.text != plain
	lines := strings.Split(plain, "\n")
	cleaned := false
	for i, line := range lines {
		if clean := cleanLine(line); clean != line {
			lines[i], cleaned = clean, true
		}
	}
	plain = strings.Join(lines, "\n")
	speakers := 0
	for _, line := range lines {
		if speakerLine.MatchString(line) {
			speakers++
		}
	}
	multi := speakers >= 2
	display := strings.TrimSpace(strings.Join(lines, " "))

	var changes []string
	if markupChanged {
		changes = append(changes, "markup_removed_or_newline_decoded")
	}
	if cleaned {
		changes = append(changes, "spaces_and_speaker_marks_cleaned")
	}
	if display != plain {
		changes = append(changes, "whitespace_flattened")
	}
	// Long two-speaker cues keep the source speaker separation.
	limit := 65
	if hanText.MatchString(display) {
		limit = 25
	}
	if multi && countedLength(display) > limit {
		display = strings.Join(lines, "\n")
		changes = append(changes, "speaker_line_breaks_kept")
	}

	top := alignment >= 7 || (position != nil && position[1] < 540)
	if changes == nil {
		changes = []string{}
	}
	return Cue{
		ID:            id,
		Start:         cue.start,
		End:           cue.end,
		OriginalStart: cue.start,
		OriginalEnd:   cue.end,
		Raw:           cue.text,
		Plain:         plain,
		Display:       display,
		Style:         cue.style,
		Alignment:     alignment,
		Position:      position,
		Top:           top,
		MultiSpeaker:  multi,
		Changes:       changes,
	}
}
