// subtitle-merge runs the production native merger on isolated inputs without
// starting the webhook server or modifying media sidecars.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/internal/subtitlellm"
	"github.com/fusionn/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	logger.Init(false)
	defer logger.Sync()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return execute(ctx)
}

func execute(ctx context.Context) error {
	configPath := flag.String("config", "config/config.yaml", "fusionn configuration")
	video := flag.String("video", "", "optional video for extraction and TV geometry")
	en := flag.String("en", "", "English SRT/ASS input (otherwise extract --en-stream)")
	zh := flag.String("zh", "", "Chinese SRT/ASS input (otherwise extract --zh-stream)")
	enStream := flag.Int("en-stream", -1, "absolute English stream index")
	zhStream := flag.Int("zh-stream", -1, "absolute Chinese stream index")
	mappingPath := flag.String("mapping", "", "import a verified existing mapping instead of API requests")
	out := flag.String("out", "", "new isolated output directory, must not exist")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if cfg.Subtitle.Engine != config.EngineLLM {
		return fmt.Errorf("subtitle-merge requires subtitle.engine: llm; the server also supports duosubs")
	}
	if *mappingPath != "" {
		cfg.Subtitle.LLM.MappingPath = *mappingPath
	}
	cfg.Subtitle.Enabled = true
	if err := cfg.Validate(); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	if err := os.Mkdir(*out, 0755); err != nil {
		return err
	}
	started := time.Now()
	width, height := 0, 0
	var probe *executor.FFProbeOutput
	if *video != "" {
		probe, err = executor.FFProbe(ctx, *video)
		if err != nil {
			return err
		}
		for _, s := range probe.Streams {
			if s.CodecType == "video" && s.Disposition["attached_pic"] != 1 {
				width, height = s.Width, s.Height
				break
			}
		}
	}
	inputs := []*string{en, zh}
	for i, index := range []int{*enStream, *zhStream} {
		if *inputs[i] != "" {
			continue
		}
		if probe == nil || index < 0 {
			return fmt.Errorf("supply --en and --zh, or --video and both absolute stream indexes")
		}
		codec := ""
		for _, s := range probe.Streams {
			if s.Index == index && s.CodecType == "subtitle" {
				codec = s.CodecName
			}
		}
		lang := []string{"en", "zh"}[i]
		ext := ".srt"
		if codec == "ass" || codec == "ssa" {
			ext = ".ass"
		}
		switch codec {
		case "subrip", "ass", "ssa", "webvtt", "mov_text", "text":
		default:
			return fmt.Errorf("stream %d is not a supported text subtitle", index)
		}
		dest := filepath.Join(*out, lang+".source"+ext)
		if ext == ".ass" {
			err = executor.ExtractSubtitleNative(ctx, *video, index, dest)
		} else {
			err = executor.ExtractSubtitle(ctx, *video, index, dest)
		}
		if err != nil {
			return err
		}
		*inputs[i] = dest
	}
	bundle, err := subtitlellm.LoadSources(*en, *zh)
	if err != nil {
		return err
	}
	mapping, audit, err := subtitlellm.NewEngine().Align(ctx, bundle, cfg.Subtitle.LLM)
	if err != nil {
		return err
	}
	data, err := subtitlellm.Render(bundle, mapping, cfg.Subtitle.LLM, cfg.Subtitle.ASSStyle, width, height)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "bilingual.ass"), data, 0644); err != nil {
		return err
	}
	for name, value := range map[string]any{"sources.json": bundle, "alignment/mapping.json": mapping, "alignment/audit.json": audit, "run.json": map[string]any{"seconds": time.Since(started).Seconds(), "width": width, "height": height, "source_fingerprint": bundle.Fingerprint, "http_attempts": audit.HTTPAttempts, "mapping_groups": len(mapping.Groups)}} {
		target := filepath.Join(*out, name)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, append(raw, '\n'), 0600); err != nil {
			return err
		}
	}
	fmt.Printf("ASS: %s (HTTP requests: %d, cache hits: %d)\n", filepath.Join(*out, "bilingual.ass"), audit.HTTPAttempts, audit.CacheHits)
	return nil
}
