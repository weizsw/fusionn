## ADDED Requirements

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

## MODIFIED Requirements

None - all new requirements are additions to the existing subtitle-styling capability.
