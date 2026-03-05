## 1. Scoring Infrastructure

- [x] 1.1 Add helper functions to read disposition flags from `StreamInfo.Disposition` (`isForced`, `isHearingImpaired`)
- [x] 1.2 Add helper functions to read frame/byte stats from `StreamInfo.Tags` (`getFrameCount`, `getByteCount`) returning `(int, bool)` for missing-value safety
- [x] 1.3 Add title keyword matching helpers for forced detection (`isForcedByTitle`: "forced", "signs", "signs & songs") and SDH detection (`isSDHByTitle`: "sdh", "cc", "hearing impaired")

## 2. Penalty Scoring Logic

- [x] 2.1 Create `scoreEnglishTrack(stream StreamInfo, maxFrames int, maxBytes int) int` that computes total penalty across all four signal layers (disposition, title, frame heuristic, byte heuristic)
- [x] 2.2 Rewrite `detectEnglishSubtitle` as two-pass: first pass collects all English candidates and determines maxFrames/maxBytes, second pass scores each candidate and picks the lowest penalty
- [x] 2.3 Implement tie-breaking: on equal penalty prefer higher frame count, then first in stream order

## 3. Logging

- [x] 3.1 Log each English candidate's penalty breakdown during analysis (disposition penalty, title penalty, frame penalty, byte penalty, total) so selection is debuggable
- [x] 3.2 Log which track was selected and why (e.g., "Selected index=3 (penalty=0) over index=2 (penalty=200)")

## 4. Tests

- [x] 4.1 Test: forced track (disposition `forced=1`) scored higher penalty than regular track
- [x] 4.2 Test: forced track detected by title "Forced" when disposition is missing
- [x] 4.3 Test: forced track detected by frame count heuristic when both disposition and title are missing
- [x] 4.4 Test: SDH track (disposition `hearing_impaired=1`) scored higher penalty than regular but lower than forced
- [x] 4.5 Test: single English track always selected regardless of flags
- [x] 4.6 Test: tie-breaking prefers higher frame count
- [x] 4.7 Test: real-world scenario — 3 English tracks (forced/regular/SDH) selects regular

## 5. Cleanup

- [x] 5.1 Remove old `matchEnglishTrack` function (replaced by `scoreEnglishTrack`)
- [x] 5.2 Update existing analyzer tests that reference old priority values
