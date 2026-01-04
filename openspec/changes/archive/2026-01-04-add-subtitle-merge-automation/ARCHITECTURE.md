# Architecture: Extensible Subtitle Processing Pipeline

## Overview

The subtitle automation system is built on a **Pipeline Architecture** that allows easy addition of new processing steps without modifying core code. Each processing step is a self-contained **Processor** that can be enabled/disabled and configured independently.

## Design Principles

1. **Extensibility**: Easy to add new processing steps (ASS styling, timing adjustment, etc.)
2. **Composability**: Processors can be chained in any order
3. **Configurability**: Each processor can have its own configuration
4. **Conditional Execution**: Processors decide if they should run based on context
5. **Error Isolation**: Processor failures are isolated and don't affect the pipeline definition

## Pipeline Architecture

```
Webhook → ProcessingContext → Pipeline → [Processors] → Result → Notification
```

### ProcessingContext

Central state object that flows through the pipeline:

```go
type ProcessingContext struct {
    // Input
    VideoPath   string
    MediaType   string  // "movie" or "episode"
    JobID       string
    
    // Analysis
    Analysis *AnalysisResult
    
    // Processing
    EnglishSubPath  string
    ChineseSubPath  string
    MergedSubPath   string
    
    // Flags
    NeedsConversion  bool
    NeedsTranslation bool
    
    // Extensible metadata
    Metadata map[string]interface{}
}
```

### Processor Interface

```go
type Processor interface {
    Name() string
    Process(ctx context.Context, pctx *ProcessingContext) error
    ShouldRun(pctx *ProcessingContext) bool
}
```

## Core Processors (Phase 1)

### 1. AnalyzerProcessor
**Purpose**: Detect subtitle tracks using ffprobe  
**Input**: `VideoPath`  
**Output**: `Analysis` (English/Chinese track info)  
**Condition**: Always runs

### 2. ExtractorProcessor
**Purpose**: Extract subtitle tracks to SRT files  
**Input**: `Analysis`  
**Output**: `EnglishSubPath`, `ChineseSubPath`  
**Condition**: Subtitles found in analysis

### 3. ConversionProcessor
**Purpose**: Convert Traditional → Simplified Chinese (OpenCC)  
**Input**: `ChineseSubPath`  
**Output**: Updated `ChineseSubPath` (converted)  
**Condition**: `NeedsConversion == true`

### 4. MergerProcessor
**Purpose**: Merge English + Chinese using DuoSubs  
**Input**: `EnglishSubPath`, `ChineseSubPath`  
**Output**: `MergedSubPath` (bilingual ASS)  
**Condition**: Both subtitles available

### 5. OutputProcessor
**Purpose**: Copy merged subtitle to video directory  
**Input**: `MergedSubPath`  
**Output**: Final subtitle file  
**Condition**: Merge successful

### 6. NotificationProcessor
**Purpose**: Send success/failure notification via Apprise  
**Input**: Processing result  
**Output**: Notification sent  
**Condition**: Apprise enabled

### 7. CleanupProcessor
**Purpose**: Remove temporary files  
**Input**: All temp file paths  
**Output**: Cleanup completed  
**Condition**: Always runs (even on error)

### 8. TranslationQueueProcessor
**Purpose**: Queue translation job if Chinese missing  
**Input**: `NeedsTranslation` flag  
**Output**: Redis queue job  
**Condition**: `NeedsTranslation == true`

## Future Processors (Extensibility Examples)

### 9. StyleProcessor
**Purpose**: Modify ASS styling (fonts, colors, positioning)  
**Configuration**:
```yaml
subtitle:
  processors:
    style:
      enabled: true
      font_family: "Arial"
      primary_font_size: 48
      secondary_font_size: 36
      primary_color: "&H00FFFFFF"  # White
      secondary_color: "&H00FFFF00" # Yellow
      outline_width: 2
      shadow_depth: 1
```

### 10. TimingProcessor
**Purpose**: Adjust subtitle timing (offset, stretching)  
**Configuration**:
```yaml
subtitle:
  processors:
    timing:
      enabled: false
      offset_ms: 0       # Shift all timings
      speed_factor: 1.0  # Speed up/slow down (1.0 = no change)
```

### 11. FilterProcessor
**Purpose**: Remove unwanted subtitle lines (ads, credits)  
**Configuration**:
```yaml
subtitle:
  processors:
    filter:
      enabled: false
      remove_patterns:
        - "^Subtitle by"
        - "^Download from"
        - "^Visit.*\\.com$"
```

### 12. QualityCheckProcessor
**Purpose**: Validate subtitle quality (timing gaps, encoding)  
**Configuration**:
```yaml
subtitle:
  processors:
    quality_check:
      enabled: false
      max_gap_ms: 5000        # Warn if gaps > 5s
      check_encoding: true    # Validate UTF-8
      min_duration_ms: 500    # Minimum subtitle duration
```

### 13. BackupProcessor
**Purpose**: Backup original subtitles before modification  
**Configuration**:
```yaml
subtitle:
  processors:
    backup:
      enabled: true
      backup_dir: "/data/subtitle-backups"
      retention_days: 30
```

## Configuration Schema (Extensible)

```yaml
subtitle:
  enabled: true
  
  # Core settings
  duosubs:
    model: "sentence-transformers/LaBSE"
    device: "auto"
  
  opencc:
    enabled: true
    config: "t2s.json"
  
  # Processor pipeline configuration
  pipeline:
    # Processors run in this order
    order:
      - analyzer
      - extractor
      - conversion
      - merger
      - style        # FUTURE
      - timing       # FUTURE
      - filter       # FUTURE
      - output
      - notification
      - cleanup
      - translation_queue
  
  # Processor-specific settings
  processors:
    # Core processors
    analyzer:
      enabled: true
    
    merger:
      enabled: true
      output_same_dir: true
    
    notification:
      enabled: true
    
    # Future processors (disabled by default)
    style:
      enabled: false
      font_family: "Arial"
      primary_font_size: 48
      secondary_font_size: 36
    
    timing:
      enabled: false
      offset_ms: 0
    
    filter:
      enabled: false
      remove_patterns: []
```

## Implementation Example

### Creating the Pipeline

```go
// In main.go or service initialization
func createSubtitlePipeline(cfg *config.Config) *subtitle.Pipeline {
    pipeline := subtitle.NewPipeline()
    
    // Core processors
    pipeline.AddProcessor(NewAnalyzerProcessor(cfg))
    pipeline.AddProcessor(NewExtractorProcessor(cfg))
    pipeline.AddProcessor(NewConversionProcessor(cfg))
    pipeline.AddProcessor(NewMergerProcessor(cfg))
    
    // Future: Conditional processor addition based on config
    if cfg.Subtitle.Processors.Style.Enabled {
        pipeline.AddProcessor(NewStyleProcessor(cfg))
    }
    
    if cfg.Subtitle.Processors.Timing.Enabled {
        pipeline.AddProcessor(NewTimingProcessor(cfg))
    }
    
    // Output and cleanup
    pipeline.AddProcessor(NewOutputProcessor(cfg))
    pipeline.AddProcessor(NewNotificationProcessor(cfg))
    pipeline.AddProcessor(NewCleanupProcessor(cfg))
    pipeline.AddProcessor(NewTranslationQueueProcessor(cfg))
    
    return pipeline
}
```

### Using the Pipeline

```go
// In webhook handler
func (h *WebhookHandler) HandleSonarr(c *gin.Context) {
    // ... parse payload ...
    
    pctx := &subtitle.ProcessingContext{
        VideoPath:  payload.EpisodeFile.Path,
        MediaType:  "episode",
        MediaTitle: payload.Series.Title,
        JobID:      jobID,
        Metadata:   make(map[string]interface{}),
    }
    
    // Execute pipeline
    if err := h.pipeline.Execute(c.Request.Context(), pctx); err != nil {
        logger.Errorf("Pipeline failed: %v", err)
        // Handle error...
    }
}
```

### Adding a New Processor (Example: Style Modification)

```go
// internal/service/subtitle/processor_style.go
type StyleProcessor struct {
    fontFamily string
    fontSize   int
    colors     StyleColors
}

func (p *StyleProcessor) Name() string {
    return "StyleProcessor"
}

func (p *StyleProcessor) ShouldRun(pctx *ProcessingContext) bool {
    // Only run if we have a merged ASS file
    return pctx.MergedSubPath != "" && 
           strings.HasSuffix(pctx.MergedSubPath, ".ass")
}

func (p *StyleProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
    logger.Infof("Modifying ASS styles: %s", pctx.MergedSubPath)
    
    // Read ASS file
    content, err := os.ReadFile(pctx.MergedSubPath)
    if err != nil {
        return fmt.Errorf("failed to read ASS file: %w", err)
    }
    
    // Modify styles (simple example)
    modified := modifyASSStyles(string(content), p.fontFamily, p.fontSize, p.colors)
    
    // Write back
    if err := os.WriteFile(pctx.MergedSubPath, []byte(modified), 0644); err != nil {
        return fmt.Errorf("failed to write modified ASS: %w", err)
    }
    
    logger.Infof("✅ ASS styles modified successfully")
    return nil
}
```

## Benefits of This Architecture

### 1. Easy Extension
Add new processors without modifying existing code:
```go
pipeline.AddProcessor(NewCustomProcessor(config))
```

### 2. Conditional Processing
Processors decide when to run:
```go
func (p *StyleProcessor) ShouldRun(pctx *ProcessingContext) bool {
    return cfg.Processors.Style.Enabled && pctx.MergedSubPath != ""
}
```

### 3. Error Handling
Pipeline catches and reports processor errors:
```go
if err := proc.Process(ctx, pctx); err != nil {
    return fmt.Errorf("processor %s failed: %w", proc.Name(), err)
}
```

### 4. Testing
Test individual processors in isolation:
```go
func TestStyleProcessor(t *testing.T) {
    proc := NewStyleProcessor(cfg)
    pctx := &ProcessingContext{
        MergedSubPath: "test.ass",
    }
    err := proc.Process(context.Background(), pctx)
    // Assert...
}
```

### 5. Debugging
Log each processor's execution:
```
▶️  Running processor: AnalyzerProcessor
✅ Processor completed: AnalyzerProcessor
▶️  Running processor: ExtractorProcessor
✅ Processor completed: ExtractorProcessor
⏭️  Skipping processor: ConversionProcessor (condition not met)
```

## Migration Path

### Phase 1 (Current)
- Implement core processors (1-8)
- Basic pipeline without processor configuration

### Phase 2 (Future)
- Add processor configuration schema
- Implement StyleProcessor
- Implement TimingProcessor

### Phase 3 (Advanced)
- Add FilterProcessor
- Add QualityCheckProcessor
- Add BackupProcessor
- Support custom user-defined processors via plugins

## Summary

This architecture provides:
- ✅ **Immediate Value**: Works for current subtitle merging needs
- ✅ **Extensibility**: Easy to add ASS styling, timing adjustments, etc.
- ✅ **Maintainability**: Each processor is self-contained and testable
- ✅ **Configurability**: Processors can be enabled/disabled and configured independently
- ✅ **Future-Proof**: Ready for plugin system if needed

The pipeline pattern is a proven design for processing workflows and aligns perfectly with the subtitle automation use case.

