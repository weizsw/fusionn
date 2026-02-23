# subtitle-styling Specification

## Purpose
TBD - created by archiving change add-custom-ass-styling. Update Purpose after archive.
## Requirements
### Requirement: Custom ASS Style Injection

The system SHALL inject custom ASS styling into merged subtitle files to provide configurable visual appearance.

**Rationale**: DuoSubs generates merged subtitles with default styling. Custom styling allows users to control fonts, colors, and appearance to match their preferences and optimize readability for bilingual content.

#### Scenario: Style injection with custom fonts and colors

**Given** a merged ASS subtitle file from DuoSubs  
**And** ASS styling is enabled in configuration  
**And** custom fonts and colors are configured  
**When** the style processor runs  
**Then** the ASS file SHALL have a custom `[Script Info]` section with WrapStyle, ScaledBorderAndShadow, Collisions, and ScriptType  
**And** the ASS file SHALL have a custom `[V4+ Styles]` section with Default and Default_1 styles  
**And** the Default style SHALL use the configured primary font, size, and color  
**And** the Default_1 style SHALL use the configured secondary font, size, and color  
**And** the `[Events]` section SHALL remain unchanged  
**And** subtitle timing SHALL be preserved exactly

#### Scenario: Style injection disabled

**Given** a merged ASS subtitle file from DuoSubs  
**And** ASS styling is disabled in configuration  
**When** the style processor runs  
**Then** the processor SHALL skip style injection  
**And** the ASS file SHALL remain in its original DuoSubs format

#### Scenario: Invalid ASS file format

**Given** a merged file that does not contain a valid `[Events]` section  
**When** the style processor attempts to parse the file  
**Then** the processor SHALL log a warning  
**And** the processor SHALL skip style injection  
**And** the pipeline SHALL continue without failing the job

#### Scenario: Configuration validation

**Given** ASS style configuration is provided  
**When** the configuration is loaded at startup  
**Then** font sizes SHALL be validated as positive integers  
**And** color values SHALL be validated as ASS format strings (&HAABBGGRR)  
**And** invalid configuration SHALL cause startup failure with descriptive error

---

### Requirement: Configurable Style Parameters

The system SHALL allow users to configure ASS styling parameters via YAML configuration.

**Rationale**: Different users have different preferences for subtitle appearance. Configuration allows customization without code changes.

#### Scenario: Configure primary style for Chinese subtitles

**Given** the subtitle configuration file  
**When** the user specifies primary_font, primary_size, and primary_color  
**Then** merged subtitles SHALL use the Default style with specified font name, size, and color  
**And** the style SHALL be applied to the first subtitle line (Chinese)

#### Scenario: Configure secondary style for English subtitles

**Given** the subtitle configuration file  
**When** the user specifies secondary_font, secondary_size, and secondary_color  
**Then** merged subtitles SHALL use the Default_1 style with specified font name, size, and color  
**And** the style SHALL be applied to the second subtitle line (English)

#### Scenario: Configure common style properties

**Given** the subtitle configuration file  
**When** the user specifies bold, outline, shadow, and margin_v  
**Then** both Default and Default_1 styles SHALL use these common properties  
**And** the properties SHALL be applied consistently across all merged subtitles

#### Scenario: Hot-reload style configuration

**Given** the service is running with ASS styling enabled  
**When** the user modifies style configuration in the YAML file  
**Then** the configuration SHALL be reloaded within 10 seconds  
**And** new merge jobs SHALL use the updated styling  
**And** existing merged subtitles SHALL remain unchanged

---

### Requirement: Pipeline Integration

The StyleProcessor SHALL be integrated into the subtitle processing pipeline after the MergerProcessor.

**Rationale**: Styling must happen after DuoSubs creates the merged file but before the file is copied to its final location.

#### Scenario: Style processor in merge pipeline

**Given** the subtitle merge pipeline is configured  
**When** the pipeline is initialized  
**Then** the StyleProcessor SHALL be positioned after MergerProcessor  
**And** the StyleProcessor SHALL be positioned before OutputProcessor  
**And** the pipeline order SHALL be: Conversion → Merger → Style → Output → Notification → Cleanup

#### Scenario: Style processor receives merged file path

**Given** a merge job is being processed  
**When** the MergerProcessor completes successfully  
**Then** the ProcessingContext SHALL contain the merged subtitle path  
**And** the StyleProcessor SHALL receive the ProcessingContext  
**And** the StyleProcessor SHALL modify the file at the merged subtitle path

#### Scenario: Style processor failure handling

**Given** the StyleProcessor encounters an error writing the styled file  
**When** the processor returns an error  
**Then** the pipeline SHALL halt execution  
**And** the job SHALL be marked as failed  
**And** the error SHALL be logged  
**And** an Apprise notification SHALL be sent with the failure details

---

### Requirement: Horizontal Margin Configuration

The system SHALL allow users to configure horizontal margins for subtitle positioning via `margin_left` and `margin_right` parameters.

**Rationale**: Different display sizes and aspect ratios benefit from adjustable horizontal margins. Users may want to keep subtitles away from screen edges or UI elements.

#### Scenario: Configure left and right margins

**WHEN** the user specifies `margin_left` and `margin_right` in ASS style configuration  
**THEN** the generated ASS styles SHALL set MarginL and MarginR to the specified values  
**AND** both Default and Default_1 styles SHALL use these margin values  
**AND** subtitles SHALL be rendered with the specified horizontal spacing from screen edges

#### Scenario: Margins not specified

**WHEN** `margin_left` or `margin_right` are not specified in configuration  
**THEN** the ASS styles SHALL use default values (MarginL=10, MarginR=10)  
**AND** subtitles SHALL render with standard horizontal margins

---

### Requirement: Subtitle Alignment Configuration

The system SHALL allow users to configure subtitle alignment via the `alignment` parameter.

**Rationale**: Users may want subtitles centered, left-aligned, or positioned in different screen locations. ASS alignment uses numeric values 1-9 representing a numpad layout (7=top-left, 8=top-center, 9=top-right, 4=middle-left, 5=middle-center, 6=middle-right, 1=bottom-left, 2=bottom-center, 3=bottom-right).

#### Scenario: Configure subtitle alignment

**WHEN** the user specifies `alignment` in ASS style configuration  
**THEN** the generated ASS styles SHALL set Alignment to the specified numeric value  
**AND** both Default and Default_1 styles SHALL use this alignment value  
**AND** subtitles SHALL be positioned according to the alignment setting

#### Scenario: Alignment not specified

**WHEN** `alignment` is not specified in configuration  
**THEN** the ASS styles SHALL use default alignment value 2 (bottom-center)  
**AND** subtitles SHALL render at the bottom center of the screen

#### Scenario: Hot-reload alignment configuration

**WHEN** the user modifies alignment in the YAML configuration file  
**THEN** the configuration SHALL be reloaded within 10 seconds  
**AND** new merge jobs SHALL use the updated alignment  
**AND** existing merged subtitles SHALL remain unchanged

---

### Requirement: Border Style Configuration

The system SHALL allow users to configure the border rendering style via the `border_style` parameter.

**Rationale**: ASS BorderStyle controls whether subtitles use an outline (BorderStyle=1) or an opaque box (BorderStyle=3). Different styles work better for different content and backgrounds.

#### Scenario: Configure border style

**WHEN** the user specifies `border_style` in ASS style configuration  
**THEN** the generated ASS styles SHALL set BorderStyle to the specified value  
**AND** both Default and Default_1 styles SHALL use this border style  
**AND** subtitles SHALL render with the specified border appearance

#### Scenario: Border style not specified

**WHEN** `border_style` is not specified in configuration  
**THEN** the ASS styles SHALL use default BorderStyle=1 (outline+shadow)  
**AND** subtitles SHALL render with outline and shadow

---

### Requirement: Text Wrapping Configuration

The system SHALL allow users to configure line wrapping behavior via the `wrap_style` parameter.

**Rationale**: ASS WrapStyle controls how long subtitle lines wrap. Users may want smart wrapping (balanced lines) or no wrapping at all depending on content and display characteristics.

#### Scenario: Configure wrap style

**WHEN** the user specifies `wrap_style` in ASS style configuration  
**THEN** the generated `[Script Info]` section SHALL set WrapStyle to the specified value  
**AND** valid values SHALL be 0 (smart wrap, top line wider), 1 (end-of-line wrap), 2 (no wrap), or 3 (smart wrap, bottom line wider)  
**AND** subtitles SHALL wrap according to the specified style

#### Scenario: Wrap style not specified

**WHEN** `wrap_style` is not specified in configuration  
**THEN** the ASS file SHALL use default WrapStyle=0 (smart wrapping)  
**AND** long subtitle lines SHALL wrap intelligently with top line wider

#### Scenario: Wrap style type handling

**WHEN** `wrap_style` is provided as a string (e.g., "0", "2") or integer (e.g., 0, 2)  
**THEN** the system SHALL parse and accept both formats  
**AND** the WrapStyle parameter SHALL be set correctly in the ASS file

---

