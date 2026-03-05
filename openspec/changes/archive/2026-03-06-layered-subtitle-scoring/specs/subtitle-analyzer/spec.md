## MODIFIED Requirements

### Requirement: English Subtitle Identification

The system SHALL identify English subtitle tracks using a layered penalty scoring system that evaluates disposition flags, title keywords, frame count, and byte size to select the best track. The priority order SHALL be: Regular English (lowest penalty) > SDH English > Forced/Signs English (highest penalty).

#### Scenario: Standard English subtitle selected over forced track

- **WHEN** a video contains English tracks at index 2 (disposition `forced=1`, 23 frames) and index 3 (no forced disposition, 567 frames)
- **THEN** the system SHALL select index 3 as the English subtitle
- **AND** the system SHALL log the penalty scores for each candidate

#### Scenario: Forced track detected by disposition flag

- **WHEN** an English subtitle track has `disposition.forced == 1`
- **THEN** the system SHALL add a penalty of 100 to that track's score

#### Scenario: Forced track detected by title keyword

- **WHEN** an English subtitle track has title containing "forced", "signs", or "signs & songs" (case-insensitive)
- **THEN** the system SHALL add a penalty of 100 to that track's score
- **AND** this title penalty SHALL stack with disposition and heuristic penalties when multiple signals match

#### Scenario: SDH track detected by disposition flag

- **WHEN** an English subtitle track has `disposition.hearing_impaired == 1`
- **THEN** the system SHALL add a penalty of 10 to that track's score

#### Scenario: SDH track detected by title keyword

- **WHEN** an English subtitle track has title containing "sdh", "cc", or "hearing impaired" (case-insensitive)
- **THEN** the system SHALL add a penalty of 10 to that track's score

#### Scenario: Frame count heuristic detects forced track

- **WHEN** multiple English subtitle tracks exist
- **AND** a track's `NUMBER_OF_FRAMES` tag is less than 25% of the maximum `NUMBER_OF_FRAMES` among all English candidates
- **THEN** the system SHALL add a penalty of 50 to that track's score

#### Scenario: Byte size heuristic detects forced track

- **WHEN** multiple English subtitle tracks exist
- **AND** a track's `NUMBER_OF_BYTES` tag is less than 25% of the maximum `NUMBER_OF_BYTES` among all English candidates
- **THEN** the system SHALL add a penalty of 50 to that track's score

#### Scenario: Frame/byte tags missing

- **WHEN** `NUMBER_OF_FRAMES` or `NUMBER_OF_BYTES` tags are not present in the stream metadata
- **THEN** the frame/byte heuristic layer SHALL contribute 0 penalty for that track
- **AND** selection SHALL rely on disposition and title keyword layers

#### Scenario: All metadata missing (worst case)

- **WHEN** an English subtitle track has no disposition flags, no title, and no frame/byte statistics
- **THEN** the system SHALL assign 0 penalty to that track
- **AND** tie-breaking SHALL prefer the track with more frames (if available), then first in stream order

#### Scenario: Tie-breaking by frame count

- **WHEN** two English subtitle tracks have identical penalty scores
- **AND** both have `NUMBER_OF_FRAMES` tags
- **THEN** the system SHALL select the track with the higher frame count

#### Scenario: Single English track (no comparison needed)

- **WHEN** only one English subtitle track exists in the video
- **THEN** the system SHALL select it regardless of disposition or title
- **AND** the frame/byte heuristic SHALL NOT apply (no peers to compare against)

#### Scenario: English SDH subtitle as fallback

- **WHEN** a video contains only a forced English track (disposition `forced=1`) and an SDH English track (disposition `hearing_impaired=1`)
- **THEN** the system SHALL select the SDH track (penalty 10 < penalty 100+)

#### Scenario: No English subtitle found

- **WHEN** no subtitle tracks match English detection criteria
- **THEN** the system SHALL return nil for English subtitle
- **AND** the system SHALL log a warning indicating English subtitle is missing
