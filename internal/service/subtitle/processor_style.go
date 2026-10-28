package subtitle

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

const (
	// metadataLLMStyled marks merged output whose styles, PlayRes and explicit
	// event positions were already produced by the LLM renderer.
	metadataLLMStyled = "llm_styled"
	// headerMarkerLLMStyled identifies an LLM-rendered file after a process
	// boundary, when ProcessingContext metadata is no longer available.
	headerMarkerLLMStyled = "; fusionn-llm-styled"
)

// StyleProcessor injects custom ASS styles into merged subtitle files.
type StyleProcessor struct {
	config config.ASSStyleConfig
	probe  func(context.Context, string) (*executor.FFProbeOutput, error)
}

// NewStyleProcessor creates a new style processor.
func NewStyleProcessor(cfg config.ASSStyleConfig) *StyleProcessor {
	return &StyleProcessor{
		config: cfg,
		probe:  executor.FFProbe,
	}
}

// Name returns the processor name.
func (p *StyleProcessor) Name() string {
	return ProcessorNameStyle
}

// ShouldRun runs if ASS styling is enabled and we have a merged subtitle.
func (p *StyleProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return p.config.Enabled && pctx.MergedSubPath != ""
}

// Process injects custom styles into the ASS file.
func (p *StyleProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Component("style")
	log.Debug(ctx, "style_detail", "Style detail", logger.Preview(pctx.MergedSubPath))

	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		return fmt.Errorf("failed to read ASS file: %w", err)
	}

	doc, err := parseASSDocument(string(content))
	if err != nil {
		log.Warn(ctx, "style_parse_failed", "Failed to parse ASS file, skipping styling", zap.Error(err))
		return nil // Don't fail the job, just skip styling
	}

	if pctx.Metadata[metadataLLMStyled] == true || doc.llmStyled {
		log.Info(ctx, "style_preserved", "Subtitle already styled by the LLM renderer, preserving it")
		return nil
	}

	styleConfig := p.config
	playResX, playResY := styleConfig.PlayResX, styleConfig.PlayResY
	if playResX <= 0 {
		playResX = assPlayResX
	}
	if playResY <= 0 {
		playResY = assPlayResY
	}
	scale, marginV := 1.0, styleConfig.MarginV
	if pctx.VideoPath != "" {
		probeOutput, probeErr := p.probe(ctx, pctx.VideoPath)
		if probeErr != nil {
			log.Warn(ctx, "style_probe_failed", "Failed to detect video resolution, using configured ASS style", zap.Error(probeErr))
		} else if width, height, ok := videoDimensions(probeOutput); ok {
			scale = referencePlaybackCanvasScale(width, height)
			marginV = adjustedMarginV(styleConfig.MarginV, width, height, playResY)
			if scale != 1 {
				log.Info(ctx, "style_adapted", "Adapting style for playback on the centered 16:9 reference canvas",
					zap.Int("width", width), zap.Int("height", height),
					zap.Float64("scale", scale), zap.Int("margin_v", marginV))
			}
		} else {
			log.Warn(ctx, "style_probe_failed", "Failed to detect video resolution, using configured ASS style")
		}
	}

	// Convert explicit event coordinates from the original script canvas before
	// replacing the header; old coordinates must be interpreted with old PlayRes.
	oldX, oldY := doc.playRes()
	doc.convertCoordinates(oldX, oldY, playResX, playResY)
	doc.replaceScriptInfo(GetScriptInfoWithPlayRes(styleConfig.WrapStyle, playResX, playResY))
	doc.upsertStyles(generateStyles(styleConfig, scale, marginV))
	doc.llmStyled = true

	if err := os.WriteFile(pctx.MergedSubPath, []byte(doc.String()), 0644); err != nil {
		return fmt.Errorf("failed to write styled ASS: %w", err)
	}

	log.Info(ctx, "style_applied", "Custom styles injected")
	return nil
}

func videoDimensions(probeOutput *executor.FFProbeOutput) (int, int, bool) {
	for _, stream := range probeOutput.Streams {
		if stream.CodecType == "video" &&
			stream.Disposition["attached_pic"] != 1 &&
			stream.Width > 0 && stream.Height > 0 {
			return stream.Width, stream.Height, true
		}
	}
	return 0, 0, false
}

func referencePlaybackCanvasScale(width, height int) float64 {
	canvasHeight := float64(width) * 9 / 16
	if float64(height) >= canvasHeight {
		return 1
	}
	return canvasHeight / float64(height)
}

// adjustedMarginV keeps the configured margin at the same proportional
// distance from the bottom edge of the centered 16:9 reference playback canvas,
// including player-added letterbox bars. playResY is the configured script
// canvas height; margins are expressed in those coordinates.
func adjustedMarginV(configuredMarginV, width, height, playResY int) int {
	scale := referencePlaybackCanvasScale(width, height)
	if scale == 1 {
		return configuredMarginV
	}

	canvasHeight := float64(height) * scale
	bottomBar := (canvasHeight - float64(height)) / 2
	verticalScale := float64(height) / float64(playResY)
	targetBottomGap := float64(configuredMarginV) * canvasHeight / float64(playResY)

	return int(math.Round((targetBottomGap - bottomBar) / verticalScale))
}

// assSection is one bracketed ASS section; lines include the header line.
type assSection struct {
	header string
	lines  []string
}

// assDocument is a minimally parsed ASS file that preserves section order,
// unknown styles, comments and event fields.
type assDocument struct {
	sections  []assSection
	llmStyled bool
}

func parseASSDocument(content string) (*assDocument, error) {
	content = strings.TrimSuffix(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	lines := strings.Split(content, "\n")
	doc := &assDocument{}
	var current *assSection
	eventsFound := false

	for _, line := range lines {
		if strings.HasPrefix(line, "[") && strings.HasSuffix(strings.TrimSpace(line), "]") {
			doc.sections = append(doc.sections, assSection{header: line})
			current = &doc.sections[len(doc.sections)-1]
			if strings.EqualFold(strings.TrimSpace(line), "[events]") {
				eventsFound = true
			}
			continue
		}
		if strings.Contains(line, headerMarkerLLMStyled) {
			doc.llmStyled = true
		}
		if current == nil {
			if strings.TrimSpace(line) == "" {
				continue
			}
			doc.sections = append(doc.sections, assSection{})
			current = &doc.sections[len(doc.sections)-1]
		}
		current.lines = append(current.lines, line)
	}

	if !eventsFound {
		return nil, errors.New("invalid ASS file: missing [Events] section")
	}
	return doc, nil
}

func (d *assDocument) section(name string) *assSection {
	for i := range d.sections {
		if strings.EqualFold(strings.TrimSpace(d.sections[i].header), name) {
			return &d.sections[i]
		}
	}
	return nil
}

// playRes returns the declared script canvas, defaulting to the legacy
// 384x288 canvas used by files produced before PlayRes became configurable.
func (d *assDocument) playRes() (int, int) {
	x, y := 384, 288
	if section := d.section("[Script Info]"); section != nil {
		for _, line := range section.lines {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n <= 0 {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "playresx":
				x = n
			case "playresy":
				y = n
			}
		}
	}
	return x, y
}

func (d *assDocument) replaceScriptInfo(scriptInfo string) {
	lines := strings.Split(strings.TrimSuffix(scriptInfo, "\n"), "\n")
	section := d.section("[Script Info]")
	if section == nil {
		d.sections = append(d.sections, assSection{})
		copy(d.sections[1:], d.sections[:len(d.sections)-1])
		d.sections[0] = assSection{header: "[Script Info]"}
		section = &d.sections[0]
	}
	// The generated text already starts with the section header.
	if len(lines) > 0 && strings.EqualFold(strings.TrimSpace(lines[0]), "[script info]") {
		lines = lines[1:]
	}
	// The marker records that positions/styles are already final, so another
	// style pass preserves the file even after a process boundary.
	if !containsString(lines, headerMarkerLLMStyled) {
		lines = append(lines, headerMarkerLLMStyled)
	}
	section.lines = lines
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

// upsertStyles replaces the shared style definitions and keeps every other
// style so dialogue events referencing DuoSubs or dual-pass styles stay valid.
func (d *assDocument) upsertStyles(generated string) {
	generatedLines := strings.Split(strings.TrimSuffix(generated, "\n"), "\n")
	var format string
	generatedStyles := map[string]bool{}
	var newStyles []string
	for _, line := range generatedLines {
		switch {
		case strings.HasPrefix(line, "Format:"):
			format = line
		case strings.HasPrefix(line, "Style:"):
			name := styleName(line)
			generatedStyles[name] = true
			newStyles = append(newStyles, line)
		}
	}

	section := d.section("[V4+ Styles]")
	if section == nil {
		newSection := assSection{
			header: "[V4+ Styles]",
			lines:  append([]string{format}, newStyles...),
		}
		// Keep the conventional Script Info, Styles, Events order.
		position := len(d.sections)
		for i := range d.sections {
			if strings.EqualFold(strings.TrimSpace(d.sections[i].header), "[events]") {
				position = i
				break
			}
		}
		d.sections = append(d.sections, assSection{})
		copy(d.sections[position+1:], d.sections[position:])
		d.sections[position] = newSection
		return
	}

	var kept []string
	for _, line := range section.lines {
		if strings.HasPrefix(line, "Style:") && generatedStyles[styleName(line)] {
			continue
		}
		if strings.HasPrefix(line, "Format:") {
			format = line
			continue
		}
		kept = append(kept, line)
	}
	lines := []string{format}
	lines = append(lines, newStyles...)
	lines = append(lines, kept...)
	section.lines = lines
}

func styleName(line string) string {
	rest := strings.TrimPrefix(line, "Style:")
	if comma := strings.Index(rest, ","); comma >= 0 {
		rest = rest[:comma]
	}
	return strings.TrimSpace(rest)
}

func (d *assDocument) String() string {
	var sb strings.Builder
	for _, section := range d.sections {
		if section.header != "" {
			sb.WriteString(section.header + "\n")
		}
		for _, line := range section.lines {
			sb.WriteString(line + "\n")
		}
	}
	return sb.String()
}

var (
	posPattern  = regexp.MustCompile(`\\pos\(\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*\)`)
	movePattern = regexp.MustCompile(`\\move\(\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)((?:\s*,\s*-?[\d.]+){0,2})\s*\)`)
)

// convertCoordinates rewrites explicit \pos and \move coordinates from the old
// script canvas to the new one. Explicit positions stay attached to the encoded
// picture, so they scale with the canvas; native \an tags carry alignment only
// and need no coordinate change. Style alignment and margins are regenerated.
func (d *assDocument) convertCoordinates(oldX, oldY, newX, newY int) bool {
	if oldX == newX && oldY == newY || oldX <= 0 || oldY <= 0 {
		return false
	}
	sx := float64(newX) / float64(oldX)
	sy := float64(newY) / float64(oldY)
	events := d.section("[Events]")
	if events == nil {
		return false
	}
	converted := false
	for i, line := range events.lines {
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}
		updated := convertDialogueLine(line, sx, sy)
		if updated != line {
			events.lines[i] = updated
			converted = true
		}
	}
	return converted
}

const dialogueTextField = 9

func convertDialogueLine(line string, sx, sy float64) string {
	parts := strings.SplitN(line, ",", dialogueTextField+1)
	if len(parts) != dialogueTextField+1 {
		return line
	}
	text := parts[dialogueTextField]
	if !strings.Contains(text, "\\") {
		return line
	}
	converted := posPattern.ReplaceAllStringFunc(text, func(match string) string {
		sub := posPattern.FindStringSubmatch(match)
		return fmt.Sprintf(`\pos(%s,%s)`, formatCoord(parseCoord(sub[1])*sx), formatCoord(parseCoord(sub[2])*sy))
	})
	converted = movePattern.ReplaceAllStringFunc(converted, func(match string) string {
		sub := movePattern.FindStringSubmatch(match)
		return fmt.Sprintf(`\move(%s,%s,%s,%s%s)`,
			formatCoord(parseCoord(sub[1])*sx), formatCoord(parseCoord(sub[2])*sy),
			formatCoord(parseCoord(sub[3])*sx), formatCoord(parseCoord(sub[4])*sy), sub[5])
	})
	if converted == text {
		return line
	}
	parts[dialogueTextField] = converted
	return strings.Join(parts, ",")
}

func parseCoord(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func formatCoord(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}
