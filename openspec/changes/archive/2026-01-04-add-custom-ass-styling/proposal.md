# Proposal: Add Custom ASS Styling

**Change ID:** `add-custom-ass-styling`  
**Status:** Proposed  
**Created:** 2025-12-16  
**Author:** System

## Why

DuoSubs generates merged subtitles with its default styling, which may not match the desired visual appearance for bilingual subtitles. We want to:

1. **Ensure consistent styling** across all merged subtitles
2. **Use a readable font** (WenQuanYi Micro Hei) optimized for Chinese characters
3. **Customize colors** for primary (Chinese) and secondary (English) text
4. **Control subtitle positioning** and appearance

The ASS format allows us to define custom styles that override DuoSubs defaults, providing:
- Better readability for bilingual content
- Consistent visual presentation
- User-configurable styling via config file

## What Changes

### New Component
- **StyleProcessor**: A new pipeline processor that modifies the merged ASS file to inject custom styles

### Modified Files
1. `internal/service/subtitle/processor_style.go` - New processor for style injection
2. `internal/service/subtitle/service.go` - Add StyleProcessor to merge pipeline
3. `internal/config/config.go` - Add ASS styling configuration
4. `config/config.example.yaml` - Add style configuration with defaults

### Configuration Schema
```yaml
subtitle:
  ass_style:
    enabled: true
    primary_font: "WenQuanYi Micro Hei"
    primary_size: 20
    primary_color: "&H00c8c8c8"
    secondary_font: "WenQuanYi Micro Hei"
    secondary_size: 13
    secondary_color: "&H0010b8ff"
    bold: true
    outline: 0.5
    shadow: 0.5
    margin_v: 5
```

## How It Works

### Pipeline Integration
```
Webhook → Analyze → Extract → Queue
                                 ↓
Worker: Convert → Merge → Style → Output → Notify → Cleanup
                           ^^^^
                         New step!
```

### Processing Flow
1. **MergerProcessor** creates merged ASS file with DuoSubs defaults
2. **StyleProcessor** (NEW) reads the ASS file and:
   - Replaces `[Script Info]` section with custom settings
   - Replaces `[V4+ Styles]` section with custom styles
   - Preserves `[Events]` section (actual subtitle content)
3. **OutputProcessor** copies styled ASS to final location

### ASS File Structure
```ass
[Script Info]
; Custom header
WrapStyle: 0
ScaledBorderAndShadow: yes
Collisions: Normal
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, ...
Style: Default,WenQuanYi Micro Hei,20,&H00c8c8c8,...    ← Chinese (larger, gray)
Style: Default_1,WenQuanYi Micro Hei,13,&H0010b8ff,...  ← English (smaller, orange)

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:00.00,0:00:05.00,Default,,0,0,0,,中文字幕
Dialogue: 0,0:00:00.00,0:00:05.00,Default_1,,0,0,0,,English subtitle
```

## Impact

### Benefits
✅ **Improved Readability**: Custom fonts and colors optimized for bilingual content  
✅ **Consistent Experience**: All merged subtitles use the same styling  
✅ **User Control**: Configurable via YAML without code changes  
✅ **Non-breaking**: Enabled by default but can be disabled  
✅ **Extensible**: Easy to add more style options in the future

### Risks/Considerations
⚠️ **ASS Format Compatibility**: Must preserve valid ASS structure  
⚠️ **DuoSubs Changes**: If DuoSubs output format changes, style injection may break  
⚠️ **Font Availability**: WenQuanYi Micro Hei must be installed on playback device

### Migration
- Existing merged subtitles: No changes (already created)
- New merges: Automatically get custom styling
- Disable: Set `subtitle.ass_style.enabled: false`

## Affected Code

### New Files
- `internal/service/subtitle/processor_style.go` - Style injection processor
- `internal/service/subtitle/style_template.go` - ASS style templates

### Modified Files
- `internal/service/subtitle/service.go` - Add StyleProcessor to pipeline
- `internal/config/config.go` - Add ASSStyleConfig
- `config/config.example.yaml` - Add style configuration

### Dependencies
- None (pure Go, no external libraries)

## Validation

### Success Criteria
1. ✅ Merged ASS files contain custom Script Info
2. ✅ Merged ASS files contain two styles: Default and Default_1
3. ✅ Subtitle events remain unchanged (no data loss)
4. ✅ Configuration hot-reload works for style changes
5. ✅ Disable flag works (original ASS preserved)
6. ✅ Invalid ASS files handled gracefully (logged, not styled)

### Testing Strategy
1. **Unit Tests**: Test ASS parsing and style injection
2. **Integration Tests**: Test full pipeline with style processor
3. **Manual Tests**: Play styled subtitles in video player

### Rollback Plan
- Set `subtitle.ass_style.enabled: false` in config
- Service automatically skips style processing
- No code deployment needed

## Open Questions

None - implementation is straightforward.

## Related Changes

- Part of the extensible pipeline architecture from `add-subtitle-merge-automation`
- Future enhancements: Style presets, per-media styling, advanced ASS features

