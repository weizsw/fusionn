## Why

The current ASS style configuration only supports basic styling (fonts, colors, outline, shadow, single vertical margin). Users need control over layout parameters like horizontal margins, alignment, border style, and wrap behavior to fine-tune subtitle positioning and appearance for different displays and preferences.

## What Changes

- Add `margin_left` and `margin_right` parameters for horizontal margin control
- Add `margin_vertical` parameter (in addition to existing `margin_v`)
- Add `alignment` parameter to control subtitle position (numeric alignment value)
- Add `border_style` parameter to control border rendering style
- Add `wrap_style` parameter to control line wrapping behavior

## Capabilities

### New Capabilities

None - this extends existing functionality.

### Modified Capabilities

- `subtitle-styling`: Add support for ASS layout parameters (margins, alignment, border style, wrap style) in addition to existing font/color parameters

## Impact

- Configuration files: `config.yaml`, `config.example.yaml`
- Configuration struct: `internal/config/config.go` (`ASSStyleConfig`)
- Style processor: `internal/service/subtitle/processor_ass_style.go` (if exists, or the style injection logic)
- Backwards compatible: New fields are optional additions
