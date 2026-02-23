## Context

The subtitle processing system uses a pipeline architecture with 11+ processors. Currently, the `pkg/logger` package wraps zap for structured logging, and processors call `logger.Info()` directly. The pipeline logs processor start/completion, but sub-logs within processors use the same format, creating a flat visual hierarchy.

Example of current output:
```
2026-02-23 21:25:37 INFO ▶️  Running processor: Merger
2026-02-23 21:25:37 INFO Merging English + Chinese subtitles with DuoSubs
2026-02-23 21:25:37 INFO Calling DuoSubs HTTP service: http://host.docker.internal:8765
2026-02-23 21:25:37 INFO   Primary (Chinese): /data/media/...
```

The `▶️  Running processor` line is emitted by pipeline.go, but the subsequent logs from within the processor aren't visually connected to it.

## Goals / Non-Goals

**Goals:**
- Each processor gets a unique emoji identifier (e.g., 🔍 Analyzer, 📤 Extractor, 🔀 Merger)
- Sub-logs within processors are indented with a consistent prefix (e.g., `  ├─ ` or similar)
- Visual hierarchy makes it easy to scan logs and identify current processing stage
- Zero breaking changes to log parsing or external integrations

**Non-Goals:**
- Not changing the underlying zap logger configuration or log levels
- Not adding structured fields (JSON) - keeping human-readable console format
- Not retroactively reformatting existing logs in files

## Decisions

### Decision 1: Processor emoji mapping
Store a mapping of processor names to emojis in the logger or pipeline layer.

**Alternatives considered:**
- Hardcode emojis in each processor → leads to inconsistency, harder to maintain
- **Selected**: Central mapping in pipeline.go → single source of truth, easy to update

**Rationale**: Pipeline already orchestrates processor execution and logging, making it the natural place for this coordination.

### Decision 2: Indentation approach
Add scoped logging helpers that prefix log lines with indent markers.

**Alternatives considered:**
- Modify all `logger.Info()` calls to manually add prefixes → error-prone, repetitive
- Thread-local context → complex, unnecessary for single-threaded pipeline execution
- **Selected**: Add `logger.Indent()` method that returns a prefixed logger → clean API, reusable

**Rationale**: Processors can call `log := logger.Indent(); log.Info("...")` for sub-logs, keeping code simple while achieving visual hierarchy.

### Decision 3: Emoji-to-processor mapping
```go
var processorEmojis = map[string]string{
    "AnalyzerProcessor":         "🔍",
    "Extractor":                 "📤",
    "Conversion":                "🔄",
    "Merger":                    "🔀",
    "Style":                     "🎨",
    "FontEmbedding":            "🔤",
    "Output":                    "💾",
    "Notification":              "📢",
    "Cleanup":                   "🧹",
    "TranslationQueue":          "🌐",
}
```

**Alternatives considered:**
- Let each processor define its own emoji → inconsistent, can't see at a glance
- **Selected**: Central registry → easy to review, maintain consistency

### Decision 4: Indent format
Use `  ├─ ` prefix for indented logs (2 spaces + tree character + space).

**Alternatives considered:**
- Simple spaces (`    `) → less visually distinct
- **Selected**: Tree-style prefix → clear visual connection, familiar pattern

## Risks / Trade-offs

**Risk**: Emoji rendering in different terminals/log aggregators
→ **Mitigation**: Use widely supported emoji (tested in common terminals). Fallback to ASCII if needed in future.

**Risk**: Indent prefix makes grep slightly harder (extra characters)
→ **Mitigation**: Benefit of readability outweighs minor grep inconvenience. Can use `grep -o` to strip prefixes if needed.

**Trade-off**: Adding emoji lookup adds minimal runtime overhead
→ **Acceptable**: Lookup is O(1) map access, negligible compared to I/O operations in processors.
