## Context

Currently, the ASS style configuration only supports visual properties (fonts, colors, bold, outline, shadow) and a single vertical margin (`margin_v`). ASS subtitle format has additional layout parameters that control positioning, wrapping, and borders. These parameters are currently hardcoded or use default values.

The existing `ASSStyleConfig` struct in `internal/config/config.go` defines the available style parameters. The style injection logic (likely in a processor) generates ASS styles and needs to be extended to include these additional parameters.

## Goals / Non-Goals

**Goals:**
- Extend ASS style configuration to support layout parameters (margins, alignment, border style, wrap style)
- Maintain backwards compatibility with existing configurations
- Keep configuration simple and well-documented

**Non-Goals:**
- Changing the style injection pipeline or processor architecture
- Adding validation beyond basic type checking (ASS format allows any numeric values)
- Supporting per-subtitle-line style overrides (this is for global default styles)

## Decisions

### Decision 1: Add fields to existing ASSStyleConfig struct

**Options considered:**
- Create a new `ASSLayoutConfig` nested struct → More organization but adds complexity
- Add fields directly to `ASSStyleConfig` → Simpler, keeps all style params together

**Choice**: Add fields directly to `ASSStyleConfig`. These are all ASS style parameters, so grouping them together is logical.

### Decision 2: Field naming convention

**Options considered:**
- Match ASS format exactly (`MarginL`, `MarginR`) → Matches ASS spec
- Use Go naming conventions (`margin_left`, `margin_right`) → Matches existing YAML style

**Choice**: Use Go/YAML conventions (`margin_left`, `margin_right`, `margin_vertical`, `alignment`, `border_style`, `wrap_style`) to be consistent with existing fields like `margin_v`, `primary_font`, etc.

### Decision 3: Handle margin_v vs margin_vertical conflict

The user provided `margin_vertical` but the existing config has `margin_v`. Both refer to the same ASS parameter (MarginV).

**Options considered:**
- Deprecate `margin_v` and replace with `margin_vertical` → Breaking change
- Support both names, with `margin_vertical` taking precedence → Backwards compatible
- Keep only `margin_v` → Ignores user's request

**Choice**: Keep `margin_v` for backwards compatibility. The user's `margin_vertical: 5` appears to be the same as the existing `margin_v: 5`, so we'll document that `margin_v` controls vertical margin.

### Decision 4: wrap_style interpretation

The user's comment shows: `wrap_style: "0" # 2: no wrap, 1: wrap`. However, ASS WrapStyle values are:
- 0: Smart wrapping (top line wider)
- 1: End-of-line wrapping
- 2: No word wrapping
- 3: Smart wrapping (bottom line wider)

**Choice**: Accept numeric values 0-3 and pass them directly to ASS WrapStyle parameter. Update config documentation to clarify ASS WrapStyle meanings.

## Risks / Trade-offs

**Risk**: Users may set invalid combinations (e.g., alignment outside 1-9 range, negative margins)
→ **Mitigation**: ASS format is forgiving - invalid values are typically ignored or clamped. We'll document valid ranges but won't add strict validation to keep config loading fast.

**Trade-off**: Adding more parameters increases configuration complexity
→ **Benefit**: Power users can fine-tune subtitle appearance. Optional parameters with sensible defaults keep simple configs simple.

**Risk**: Confusion between `margin_v` (existing) and the concept of vertical margins
→ **Mitigation**: Document clearly that `margin_v` is vertical margin, and new `margin_left`/`margin_right` are horizontal margins.
