# Summary: Add Custom ASS Styling

**Change ID:** `add-custom-ass-styling`  
**Status:** ✅ Validated  
**Estimated Effort:** ~11 hours

## What This Does

Adds a new **StyleProcessor** to the subtitle pipeline that injects custom ASS styles into merged subtitles, allowing users to control:
- Font (WenQuanYi Micro Hei optimized for Chinese)
- Size (20 for Chinese, 13 for English)
- Colors (Gray for Chinese, Orange for English)
- Bold, outline, shadow, margins

## Key Features

✅ **Configurable**: All styling via YAML, no code changes  
✅ **Hot-reload**: Config changes apply to new merges immediately  
✅ **Non-breaking**: Enabled by default, can be disabled  
✅ **Extensible**: Easy to add more style options  
✅ **Fail-safe**: Invalid ASS files logged but don't crash pipeline

## Pipeline Position

```
Webhook → Analyze → Extract → Queue
                                 ↓
Worker: Convert → Merge → Style → Output → Notify → Cleanup
                           ^^^^
                         NEW!
```

## Configuration Example

```yaml
subtitle:
  ass_style:
    enabled: true
    primary_font: "WenQuanYi Micro Hei"
    primary_size: 20
    primary_color: "&H00c8c8c8"      # Gray
    secondary_font: "WenQuanYi Micro Hei"
    secondary_size: 13
    secondary_color: "&H0010b8ff"    # Orange
    bold: true
    outline: 0.5
    shadow: 0.5
    margin_v: 5
```

## Output Style

```ass
[Script Info]
WrapStyle: 0
ScaledBorderAndShadow: yes
Collisions: Normal
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, ...
Style: Default,WenQuanYi Micro Hei,20,&H00c8c8c8,...    ← Chinese
Style: Default_1,WenQuanYi Micro Hei,13,&H0010b8ff,...  ← English

[Events]
(Preserved exactly as DuoSubs created)
```

## Files to Create

1. `internal/service/subtitle/processor_style.go` - Style injection processor
2. `internal/service/subtitle/style_template.go` - ASS style templates

## Files to Modify

1. `internal/config/config.go` - Add `ASSStyleConfig`
2. `internal/service/subtitle/service.go` - Add StyleProcessor to pipeline
3. `config/config.example.yaml` - Add style configuration

## Implementation Tasks

1. **Configuration** (1h) - Add config schema and validation
2. **Templates** (1h) - Create ASS style templates
3. **Processor** (3h) - Implement style injection logic
4. **Integration** (1h) - Add to pipeline
5. **Error Handling** (1h) - Handle edge cases
6. **Testing** (2h) - Unit + integration tests
7. **Documentation** (1h) - ASS_STYLING.md guide
8. **Validation** (1h) - End-to-end testing

## Success Criteria

✅ Merged ASS files contain custom Script Info  
✅ Merged ASS files contain Default and Default_1 styles  
✅ Subtitle events remain unchanged  
✅ Config hot-reload works  
✅ Disable flag works  
✅ Invalid ASS files handled gracefully

## Testing Strategy

- **Unit**: Style generation, ASS parsing, injection
- **Integration**: Full pipeline with style enabled/disabled
- **Manual**: Play styled subtitles in mpv/VLC

## Related Changes

- **Depends on**: `add-subtitle-merge-automation` (pipeline architecture)
- **Extends**: Extensible pipeline with new processor
- **Future**: Style presets, per-media styling, advanced ASS features

---

**Next Step**: Run `openspec apply add-custom-ass-styling` to implement!

