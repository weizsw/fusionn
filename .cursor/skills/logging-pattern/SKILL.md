---
name: logging-pattern
description: Logging conventions for fusionn using the indented logger pattern. Use when writing or modifying any Go code that produces log output, creating new processors, or adding logging to services.
---

# Logging Pattern

fusionn uses a two-tier logging hierarchy via `pkg/logger` to produce visually structured output with tree-style indentation.

## Two Tiers

### Tier 1: Top-level logs (`logger.*`)

Used in **service orchestration**, **pipeline execution**, and **startup checks**. No indent prefix.

```go
logger.Infof("📝 Analyzing subtitles for %s (job: %s)", title, jobID)
logger.Warn("⚠️ cleanit not found in PATH - SDH subtitle filtering disabled")
```

### Tier 2: Indented logs (`log := logger.Indent()`)

Used **inside processor `Process()` methods** and **analyzer internals**. Adds the `├─` prefix for visual hierarchy under the pipeline's top-level log lines.

```go
func (p *MyProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
    log := logger.Indent()
    log.Infof("Doing something: %s", path)
    // ...
    log.Info("✅ Something completed")
    return nil
}
```

## Rules

1. **Every `Process()` method** must start with `log := logger.Indent()` and use `log.*` for all logging within it.
2. **Never use `logger.*` (top-level) inside a processor's `Process()` method.** The pipeline executor already logs processor start/stop at the top level — processor internals must be indented.
3. **`ShouldRun()` methods** that need to log (e.g., binary-not-found warnings) use top-level `logger.Warn()` since they run outside the indented processor context.
4. **Startup checks** in `service.go` use top-level `logger.Warn()`.

## Emoji Conventions

| Context | Emoji | Example |
|---------|-------|---------|
| Success completion | ✅ | `log.Info("✅ SDH content filtered successfully")` |
| Warning/degraded | ⚠️ | `logger.Warn("⚠️ binary not found - feature disabled")` |
| Pipeline step start | ▶️ | (pipeline.go only) |
| Pipeline step skip | ⏭️ | (pipeline.go only) |
| Job-level context | topic emoji | `logger.Infof("📝 Analyzing subtitles...")` |

Each processor also has a mapped emoji in `pipeline.go`'s `processorEmojis` map — add an entry when creating a new processor.

## Output Example

```
INFO  📝 Analyzing subtitles for Movie.mkv (job: abc-123)
INFO  ▶️ Running processor: 🔍 AnalyzerProcessor
INFO    ├─ Analyzing subtitles in: /media/Movie.mkv
INFO    ├─ Found 3 subtitle track(s)
INFO    ├─ ✅ English subtitle detected: index=2
INFO  ✅ Processor completed: 🔍 AnalyzerProcessor
INFO  ▶️ Running processor: 📤 Extractor
INFO    ├─ Extracted English subtitle: /media/Movie.eng.srt
INFO  ✅ Processor completed: 📤 Extractor
INFO  ▶️ Running processor: 🔇 SDHFilter
INFO    ├─ Filtering SDH content from: /media/Movie.eng.srt
INFO    ├─ ✅ SDH content filtered successfully
INFO  ✅ Processor completed: 🔇 SDHFilter
```
