## ADDED Requirements

### Requirement: Notification indicates Chinese subtitle source

The system SHALL indicate whether Chinese subtitles were extracted from video or will be translated.

#### Scenario: Chinese subtitle extracted from video
- **WHEN** Chinese subtitle was found in video (NeedsTranslation is false and ChineseSubPath is set)
- **THEN** notification body includes "Chinese Sub: Extracted"

#### Scenario: Chinese subtitle will be translated
- **WHEN** Chinese subtitle was not found and translation is queued (NeedsTranslation is true)
- **THEN** notification body includes "Chinese Sub: Queued for Translation"

#### Scenario: No Chinese subtitle processing
- **WHEN** processing warns/fails before Chinese subtitle determination
- **THEN** notification body includes "Chinese Sub: Not Available" or omits this field

### Requirement: Notification shows Traditional Chinese conversion status

The system SHALL indicate when Traditional to Simplified Chinese conversion occurred.

#### Scenario: Traditional to Simplified conversion applied
- **WHEN** Chinese subtitle required Traditional to Simplified conversion (NeedsConversion is true)
- **THEN** notification body includes "Conversion: Traditional → Simplified Chinese"

#### Scenario: No conversion needed
- **WHEN** Chinese subtitle did not require conversion (NeedsConversion is false)
- **THEN** notification body omits conversion status line

### Requirement: Notification uses readable multi-line format

The system SHALL format notification messages with labeled fields on separate lines for readability.

#### Scenario: Success notification formatting
- **WHEN** subtitle merge completes successfully
- **THEN** notification body uses format:
  ```
  Media: <title>
  Type: <movie/episode>
  Chinese Sub: <Extracted/Queued for Translation>
  [Conversion: Traditional → Simplified Chinese]
  ```

#### Scenario: Queued notification formatting
- **WHEN** subtitle is queued for translation
- **THEN** notification body uses format:
  ```
  Media: <title>
  Type: <movie/episode>
  Chinese Sub: Queued for Translation
  Reason: Chinese subtitle missing
  ```

#### Scenario: Warning notification formatting
- **WHEN** no subtitles are processed
- **THEN** notification body uses format:
  ```
  Media: <title>
  Type: <movie/episode>
  Reason: Required subtitles not found
  ```

#### Scenario: Labels are consistent across notification types
- **WHEN** any notification is sent
- **THEN** field labels use consistent capitalization and format (e.g., "Media:", "Type:", "Chinese Sub:", "Conversion:", "Reason:")
