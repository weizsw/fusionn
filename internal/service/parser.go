package service

import (
	"bytes"
	"context"
	"fmt"
	"fusionn/config"
	"fusionn/errs"
	"fusionn/internal/consts"
	"fusionn/internal/model"
	"fusionn/internal/mq"
	"fusionn/logger"
	"fusionn/utils"
	"os"
	"regexp"
	"sort"
	"strings"

	astisub "github.com/asticode/go-astisub"
	"go.uber.org/zap"
)

type Parser interface {
	Parse(input string) (*astisub.Subtitles, error)
	ParseFromBytes(ctx context.Context, stream *model.ExtractedStream) (*model.ParsedSubtitles, error)
	RemoveSDH(subs *astisub.Subtitles) *astisub.Subtitles
	Clean(subs *astisub.Subtitles) *astisub.Subtitles
	ParseFile(input string) (*ASSContent, error)
	ExportFile(ass *ASSContent, filePath string) error
}

type parser struct {
	convertor Convertor
	ffmpeg    FFmpeg
	q         mq.MessageQueue
	facade    Facade
}

func NewParser(c Convertor, f FFmpeg, q mq.MessageQueue, facade Facade) *parser {
	return &parser{convertor: c, ffmpeg: f, q: q, facade: facade}
}

// ASSContent represents the parsed content of an ASS file
type ASSContent struct {
	ScriptInfo []string
	Styles     []string
	Events     []string
	Fonts      []string
	Raw        []string
}

func (p *parser) ExportFile(ass *ASSContent, filePath string) error {
	res := []string{}
	res = append(res, "[Script Info]")
	res = append(res, ass.ScriptInfo...)
	res = append(res, "") // Add empty line between blocks
	res = append(res, "[V4+ Styles]")
	res = append(res, ass.Styles...)
	res = append(res, "") // Add empty line between blocks
	res = append(res, "[Fonts]")
	res = append(res, ass.Fonts...)
	res = append(res, "") // Add empty line between blocks
	res = append(res, "[Events]")
	res = append(res, ass.Events...)

	return os.WriteFile(filePath, []byte(strings.Join(res, "\n")), 0644)
}

func (p *parser) ParseFile(input string) (*ASSContent, error) {
	if input == "" {
		logger.S.Info("input is empty")
		return nil, fmt.Errorf("input file path is empty")
	}

	// Read file content
	content, err := os.ReadFile(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Basic validation of file content
	if len(content) == 0 {
		return nil, fmt.Errorf("file is empty")
	}

	// Check if it's a text file by looking for null bytes
	if bytes.IndexByte(content, 0) != -1 {
		return nil, fmt.Errorf("file contains binary data")
	}

	// Split content into lines
	lines := strings.Split(string(content), "\n")

	result := &ASSContent{
		Raw: lines,
	}

	currentSection := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch line {
		case "[Script Info]":
			currentSection = "script_info"
		case "[V4+ Styles]", "[V4 Styles]":
			currentSection = "styles"
		case "[Fonts]":
			currentSection = "fonts"
		case "[Events]":
			currentSection = "events"
		default:
			switch currentSection {
			case "script_info":
				result.ScriptInfo = append(result.ScriptInfo, line)
			case "styles":
				result.Styles = append(result.Styles, line)
			case "fonts":
				result.Fonts = append(result.Fonts, line)
			case "events":
				result.Events = append(result.Events, line)
			}
		}
	}

	// Validate that we found all required sections
	if len(result.ScriptInfo) == 0 || len(result.Styles) == 0 || len(result.Events) == 0 {
		return nil, fmt.Errorf("invalid ASS format: missing required sections content")
	}

	return result, nil
}

func (p *parser) Parse(input string) (*astisub.Subtitles, error) {
	if input == "" {
		logger.S.Infof("input is empty")
		return nil, nil
	}
	s, err := astisub.OpenFile(input)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (p *parser) ParseFromBytes(ctx context.Context, stream *model.ExtractedStream) (*model.ParsedSubtitles, error) {
	if len(stream.EngSubBuffer) == 0 && len(stream.SdhSubBuffer) == 0 {
		logger.S.Info("input data is empty")
		return nil, nil
	}

	parsedSubtitles := &model.ParsedSubtitles{
		FileName: stream.FileName,
		FilePath: stream.FilePath,
		EngIndex: stream.EngIndex,
	}

	var err error
	var engSub, chsSub, chtSub, sdhSub *astisub.Subtitles

	// Parse English subtitles if available
	if len(stream.EngSubBuffer) > 0 {
		engSub, err = astisub.ReadFromSRT(bytes.NewReader(stream.EngSubBuffer))
		if err != nil {
			return nil, fmt.Errorf("failed to parse English subtitles: %w", err)
		}
		parsedSubtitles.EngSubtitle = engSub
	}

	if len(stream.SdhSubBuffer) > 0 {
		sdhSub, err = astisub.ReadFromSRT(bytes.NewReader(stream.SdhSubBuffer))
		if err != nil {
			return nil, fmt.Errorf("failed to parse SDH subtitles: %w", err)
		}
		parsedSubtitles.SdhSubtitle = sdhSub
	}

	if len(stream.SdhSubBuffer) > 0 && len(stream.EngSubBuffer) == 0 {
		engSub := p.RemoveSDH(sdhSub)
		parsedSubtitles.EngSubtitle = engSub
	}

	// Try to get Chinese simplified subtitles
	if len(stream.ChsSubBuffer) > 0 {
		chsSub, err = astisub.ReadFromSRT(bytes.NewReader(stream.ChsSubBuffer))
		if err != nil {
			return nil, fmt.Errorf("failed to parse Chinese simplified subtitles: %w", err)
		}

		if config.C.General.ForceSimplified {
			chsSub, err = p.convertor.ConvertToSimplified(chsSub)
			if err != nil {
				return nil, fmt.Errorf("failed to convert traditional to simplified: %w", err)
			}
		}
		parsedSubtitles.ChsSubtitle = chsSub
	} else if len(stream.ChtSubBuffer) > 0 {
		chtSub, err = astisub.ReadFromSRT(bytes.NewReader(stream.ChtSubBuffer))
		if err != nil {
			return nil, fmt.Errorf("failed to parse Chinese traditional subtitles: %w", err)
		}

		chsSub, err = p.convertor.ConvertToSimplified(chtSub)
		if err != nil {
			return nil, fmt.Errorf("failed to convert traditional to simplified: %w", err)
		}
		parsedSubtitles.ChsSubtitle = chsSub
	} else if engSub != nil {
		if config.C.Translate.Provider == "llm" {
			err := p.translateToSimplifiedAsync(ctx, stream)
			if err != nil {
				logger.L.Error("failed sending translate job to queue", zap.Error(err))
			}
			return parsedSubtitles, errs.ErrStopPipeline
		}
		chsSub, err = p.convertor.TranslateToSimplified(engSub)
		if err != nil {
			return nil, fmt.Errorf("failed to translate English to simplified: %w", err)
		}
		parsedSubtitles.ChsSubtitle = chsSub
		parsedSubtitles.Translated = true
	}

	return parsedSubtitles, nil
}

func (p *parser) translateToSimplifiedAsync(ctx context.Context, info *model.ExtractedStream) error {
	outputPath := utils.ReplaceExtension(info.FilePath, fmt.Sprintf("%s.srt", consts.ENG_LAN))
	err := p.ffmpeg.ExtractStream(info.FilePath, outputPath, info.EngIndex)
	if err != nil {
		return err
	}

	overview := ""
	if config.C.TVDB.Enabled && info.TVDBSeriesID > 0 {
		overview, err = p.facade.GetSeriesEpisodeOverview(ctx, info.TVDBSeriesID, info.Season, info.Episode)
		if err != nil {
			return err
		}
	}

	msg := mq.Message{
		FileName: info.FileName,
		Path:     outputPath,
		Provider: config.C.GPTSubTrans.Provider,
	}
	if overview != "" {
		msg.Overview = overview
	}

	err = p.q.Publish(ctx, consts.TRANSLATE_QUEUE, msg)
	if err != nil {
		return err
	}

	return nil
}

func (p *parser) RemoveSDH(subs *astisub.Subtitles) *astisub.Subtitles {
	sdhPattern := regexp.MustCompile(`\[.*?\]|\(.*?\)`)
	cleanedSubs := &astisub.Subtitles{}

	// Iterate over the subtitles and remove SDH text
	for _, item := range subs.Items {
		cleanedItem := *item // Create a copy of the item to avoid modifying the original
		shouldAdd := false
		for _, line := range cleanedItem.Lines {
			for i := range line.Items {
				line.Items[i].Text = strings.TrimSpace(sdhPattern.ReplaceAllString(line.Items[i].Text, ""))
				if line.Items[i].Text != "" {
					shouldAdd = true
				}
			}
		}
		if shouldAdd {
			cleanedSubs.Items = append(cleanedSubs.Items, &cleanedItem)
		}
	}
	return cleanedSubs
}

func (p *parser) Clean(subs *astisub.Subtitles) *astisub.Subtitles {
	if subs == nil || len(subs.Items) == 0 {
		return subs
	}

	// First sort items by start time to ensure proper overlap detection
	sort.Slice(subs.Items, func(i, j int) bool {
		return subs.Items[i].StartAt < subs.Items[j].StartAt
	})

	var cleanedItems []*astisub.Item

	for i, item := range subs.Items {
		// Merge multiple lines into one for the current item
		if len(item.Lines) > 1 {
			mergedText := ""
			for _, line := range item.Lines {
				for _, lineItem := range line.Items {
					mergedText += strings.TrimSpace(lineItem.Text) + " "
				}
			}
			item.Lines = []astisub.Line{{
				Items: []astisub.LineItem{{
					Text: strings.TrimSpace(mergedText),
				}},
			}}
		}

		// For first item, just add it
		if i == 0 {
			cleanedItems = append(cleanedItems, item)
			continue
		}

		// Get previous item (our current working item)
		prevItem := cleanedItems[len(cleanedItems)-1]

		// Check for overlap with previous item
		if item.StartAt < prevItem.EndAt {
			// Merge overlapping subtitles into previous item
			mergedText := prevItem.Lines[0].Items[0].Text + " " + item.Lines[0].Items[0].Text
			prevItem.Lines[0].Items[0].Text = strings.TrimSpace(mergedText)
			// Extend end time if necessary
			if item.EndAt > prevItem.EndAt {
				prevItem.EndAt = item.EndAt
			}
		} else {
			// No overlap, add as new item
			cleanedItems = append(cleanedItems, item)
		}
	}

	subs.Items = cleanedItems
	return subs
}
