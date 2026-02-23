## 1. Update Success Notification Format

- [x] 1.1 Add "Chinese Sub: Extracted" or "Chinese Sub: Queued for Translation" based on NeedsTranslation flag to success notification
- [x] 1.2 Add "Conversion: Traditional → Simplified Chinese" line when NeedsConversion is true
- [x] 1.3 Keep existing fields: Media, Type
- [x] 1.4 Remove Output field from notification body

## 2. Update Info (Queued) Notification Format

- [x] 2.1 Add "Chinese Sub: Queued for Translation" field
- [x] 2.2 Keep existing fields: Media, Type, Reason

## 3. Update Warning Notification Format

- [x] 3.1 Add "Chinese Sub: Not Available" field when applicable
- [x] 3.2 Keep existing fields: Media, Type, Reason

## 4. Testing & Verification

- [x] 4.1 Test success notification with extracted Chinese subtitle (no conversion)
- [x] 4.2 Test success notification with extracted Chinese subtitle requiring Traditional→Simplified conversion
- [x] 4.3 Test queued notification when Chinese subtitle is missing
- [x] 4.4 Test warning notification when required subtitles not found
- [x] 4.5 Verify message length stays under 500 characters for all scenarios
