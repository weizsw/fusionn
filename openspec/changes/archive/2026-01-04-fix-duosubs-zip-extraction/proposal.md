# Proposal: Fix DuoSubs Zip Extraction

**Change ID:** `fix-duosubs-zip-extraction`  
**Status:** Proposed  
**Created:** 2026-01-04  
**Author:** System

## Why

The current DuoSubs integration is incorrect. DuoSubs outputs a **zip file** containing three ASS files, not a single ASS file:

- `{basename}_combined.ass` ← **The one we need**
- `{basename}_primary.ass`
- `{basename}_secondary.ass`

Our current implementation incorrectly:

1. Uses wrong command format (missing `merge` subcommand)
2. Uses wrong parameter names (`-e`/`-s` instead of `-p`/`-s`)
3. Expects a single ASS file instead of a zip file
4. Doesn't extract the zip or select the combined file

This causes subtitle merging to fail completely.

## What Changes

### Modified Components

- **`internal/executor/duosubs.go`** - Fix command format and add zip extraction
- **`Dockerfile`** - Add Python, DuoSubs, ffmpeg dependencies
- **`docker-compose.yaml`** - Add HuggingFace cache volume mount
- **`config/config.example.yaml`** - Document model persistence

### New Functionality

1. Use correct DuoSubs command: `duosubs merge -p <chinese> -s <english>`
2. Extract the generated zip file
3. Locate and return the `*_combined.ass` file
4. Clean up temporary zip and extracted files
5. Add proper error handling for zip extraction failures
6. Make model parameter optional (use DuoSubs default if empty)
7. Add Docker support for DuoSubs with model caching

### Configuration Changes

- Allow `subtitle.duosubs.model` to be empty (uses DuoSubs default: LaBSE)
- Document HuggingFace cache volume mount in docker-compose.yaml

## How It Works

### Current (Incorrect) Flow

```
MergerProcessor
  ↓
executor.MergeDuoSubs(eng, chs, outputDir)
  ↓
Execute: duosubs -e <eng> -s <chs> -o <outputDir>  ❌ WRONG
  ↓
Expect: outputDir/output.ass  ❌ DOESN'T EXIST
  ↓
FAIL
```

### New (Correct) Flow

```
MergerProcessor
  ↓
executor.MergeDuoSubs(eng, chs, outputDir)
  ↓
Execute: duosubs merge -p <chs> -s <eng> --output-dir <outputDir> ✅
  ↓
DuoSubs creates: outputDir/basename.zip
  ↓
Extract zip → find basename_combined.ass ✅
  ↓
Return: outputDir/basename_combined.ass ✅
  ↓
Clean up zip file
```

## Impact

### Benefits

✅ **Fixes subtitle merging** - Actually works with real DuoSubs  
✅ **Proper file handling** - Extracts zip correctly  
✅ **Clean temporary files** - Removes zip after extraction  
✅ **Better error messages** - Reports zip extraction failures  
✅ **Docker ready** - Includes all Python dependencies  
✅ **Model caching** - Volume mount prevents re-downloading models  
✅ **Offline capable** - Models persist across container restarts

### Risks

⚠️ **Breaking Change**: The function signature changes slightly (no longer needs output path guessing)  
⚠️ **Zip Dependency**: Requires Go's `archive/zip` (standard library, no issue)  
⚠️ **Image Size**: Dockerfile adds ~200MB (Python + deps), but models stay in volume  
⚠️ **First Run**: Takes 5-10 minutes to download LaBSE model (~2GB) on first use

### Migration

- This is a bug fix for non-working code
- No user-facing config changes needed
- Existing deployments will start working correctly

## Affected Code

### Modified Files

- `internal/executor/duosubs.go` - Fix command and add zip extraction
- `internal/service/subtitle/processor_merger.go` - Update to handle new return path
- `Dockerfile` - Add Python, DuoSubs, ffmpeg, opencc dependencies
- `docker-compose.yaml` - Add HuggingFace cache volume
- `config/config.example.yaml` - Add Docker volume documentation

### Dependencies

- `archive/zip` (Go standard library)
- `path/filepath` (already used)
- **Docker**: `python3`, `py3-pip`, `ffmpeg`, `opencc`
- **Python**: `duosubs` package

## Validation

### Success Criteria

1. ✅ DuoSubs merge subcommand executes correctly
2. ✅ Zip file is created and extracted
3. ✅ Combined ASS file is located and returned
4. ✅ Temporary zip is cleaned up
5. ✅ Error handling works for missing/corrupt zips

### Testing Strategy

1. **Unit Tests**: Mock zip creation/extraction
2. **Integration Tests**: Test with real DuoSubs (if available)
3. **Manual Tests**: Run full pipeline with actual video files

## Docker Model Persistence Strategy

### HuggingFace Model Caching (Option 1 - Chosen)

DuoSubs downloads SentenceTransformer models from HuggingFace (~2GB for LaBSE). These are cached in `/root/.cache/huggingface/` by default.

**Solution: Volume Mount**

```yaml
services:
  fusionn:
    volumes:
      - huggingface-cache:/root/.cache/huggingface
      
volumes:
  huggingface-cache:
```

**Behavior:**

- **First run**: Downloads model (~2GB, takes 5-10 minutes)
- **Subsequent runs**: Uses cached model (instant startup)
- **Across restarts**: Model persists in Docker volume

**Alternatives Considered:**

- ❌ Pre-bake model in image: +2GB image size, slower builds, harder to update
- ❌ No persistence: Re-downloads every time (slow, wasteful)
- ✅ Volume mount: Best balance of size, speed, and flexibility

## Open Questions

None - the DuoSubs behavior is well-documented and clear.

## Related Changes

- Fixes implementation from `add-subtitle-merge-automation` (archived)
- Updates spec `subtitle-merger`
