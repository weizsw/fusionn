# Tasks: Add Custom ASS Styling

**Change ID:** `add-custom-ass-styling`

## Task List

### 1. Configuration Schema
- [x] **1.1** Add `ASSStyleConfig` struct to `internal/config/config.go`
  - Fields: `Enabled`, `PrimaryFont`, `PrimarySize`, `PrimaryColor`, `SecondaryFont`, `SecondarySize`, `SecondaryColor`, `Bold`, `Outline`, `Shadow`, `MarginV`
  - Validation: Font size > 0, colors in ASS format
- [x] **1.2** Add `ASSStyle` field to `SubtitleConfig` struct
- [x] **1.3** Update `config/config.example.yaml` with default style configuration
  - Document each field with comments
  - Provide safe defaults matching the provided style
- [x] **1.4** Add config validation tests

### 2. ASS Style Template
- [x] **2.1** Create `internal/service/subtitle/style_template.go`
  - Define Script Info template
  - Define V4+ Styles template with placeholders
  - Add helper to generate style section from config
- [x] **2.2** Add unit tests for template generation
  - Test with various font sizes and colors
  - Test with enabled/disabled bold

### 3. Style Processor
- [x] **3.1** Create `internal/service/subtitle/processor_style.go`
  - Implement `StyleProcessor` struct
  - Implement `ShouldRun()`: Check if ASS styling enabled
  - Implement `Process()`: Read ASS, inject styles, write ASS
- [x] **3.2** Implement ASS parsing logic
  - Read ASS file into memory
  - Split into sections: Script Info, V4+ Styles, Events
  - Validate ASS structure
- [x] **3.3** Implement style injection logic
  - Replace Script Info section
  - Replace V4+ Styles section
  - Preserve Events section exactly
  - Handle malformed ASS files gracefully
- [x] **3.4** Add comprehensive unit tests
  - Test with valid ASS files
  - Test with malformed ASS files
  - Test with style disabled
  - Test with various config values

### 4. Pipeline Integration
- [x] **4.1** Update `internal/service/subtitle/service.go`
  - Add `StyleProcessor` to merge pipeline after `MergerProcessor`
  - Pass config to StyleProcessor constructor
- [x] **4.2** Verify pipeline order:
  ```
  Conversion → Merger → Style → Output → Notification → Cleanup
  ```
- [x] **4.3** Test pipeline integration
  - Ensure style processor runs after merge
  - Verify output files have custom styles

### 5. Error Handling
- [x] **5.1** Handle missing ASS file (should not happen, but defensive)
- [x] **5.2** Handle invalid ASS format (log warning, skip styling)
- [x] **5.3** Handle write errors (log error, fail job)
- [x] **5.4** Add error messages to Apprise notifications

### 6. Testing
- [x] **6.1** Unit tests for StyleProcessor
  - Test style injection
  - Test ASS parsing
  - Test error cases
- [x] **6.2** Integration test: Full pipeline with style
  - Create test ASS file
  - Run through pipeline
  - Verify styled output
- [x] **6.3** Manual test: Play styled subtitle
  - Test with mpv/VLC
  - Verify fonts render correctly
  - Verify colors match config

### 7. Documentation
- [x] **7.1** Update `FINAL_SUMMARY.md` with style feature
- [x] **7.2** Create `ASS_STYLING.md` with:
  - How ASS styling works
  - Configuration options
  - Color format reference (&HAABBGGRR)
  - Font recommendations
  - Troubleshooting guide
- [x] **7.3** Update `config/config.example.yaml` comments
- [x] **7.4** Add style customization examples

### 8. Validation
- [x] **8.1** Run full pipeline end-to-end
- [x] **8.2** Verify styled ASS plays correctly in video players
- [x] **8.3** Test with config hot-reload (change colors, verify next merge uses new style)
- [x] **8.4** Test with style disabled (verify original ASS preserved)
- [x] **8.5** Run all unit tests (`make test`)
- [x] **8.6** Run all integration tests
- [x] **8.7** Build succeeds (`make build`)
- [x] **8.8** No linter errors

## Task Dependencies

```
1.1 → 1.2 → 1.3 → 1.4
2.1 → 2.2
3.1 → 3.2 → 3.3 → 3.4
4.1 → 4.2 → 4.3
5.1, 5.2, 5.3, 5.4 (parallel with 3.x)
6.1, 6.2, 6.3 (after 4.3)
7.1, 7.2, 7.3, 7.4 (parallel, after 6.x)
8.1 → 8.2 → 8.3 → 8.4 → 8.5 → 8.6 → 8.7 → 8.8
```

## Estimated Effort

- **Configuration**: 1 hour
- **Style Template**: 1 hour
- **Style Processor**: 3 hours
- **Pipeline Integration**: 1 hour
- **Error Handling**: 1 hour
- **Testing**: 2 hours
- **Documentation**: 1 hour
- **Validation**: 1 hour

**Total**: ~11 hours

