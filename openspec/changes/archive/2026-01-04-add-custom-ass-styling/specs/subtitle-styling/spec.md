# Spec Delta: Subtitle Styling

**Capability:** `subtitle-styling`  
**Change ID:** `add-custom-ass-styling`

## ADDED Requirements

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

## Relationships

**Extends**: `add-subtitle-merge-automation` (uses the extensible pipeline architecture)  
**Requires**: Merged ASS file from DuoSubs  
**Modifies**: ASS file structure (Script Info and V4+ Styles sections only)  
**Preserves**: ASS Events section (subtitle content and timing)

