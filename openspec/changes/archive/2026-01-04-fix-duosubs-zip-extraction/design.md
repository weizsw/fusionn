# Design: Fix DuoSubs Zip Extraction

**Change ID:** `fix-duosubs-zip-extraction`

## Problem Statement

DuoSubs outputs a zip file containing three ASS files, but our implementation expects a single ASS file. This causes merge failures.

## Design Goals

1. **Correct DuoSubs Integration** - Use proper command format
2. **Automatic Zip Handling** - Extract and select combined file transparently
3. **Clean Temporary Files** - Remove zip after extraction
4. **Robust Error Handling** - Detect and report extraction failures

## Solution Architecture

### DuoSubs Command Format

**Current (Wrong):**
```bash
duosubs -e english.srt -s chinese.srt -o /output/dir
```

**Correct:**
```bash
duosubs merge -p chinese.srt -s english.srt --output-dir /output/dir --output-name basename
```

**Parameters:**
- `-p, --primary` - Primary language (Chinese in our case)
- `-s, --secondary` - Secondary language (English)
- `--output-dir` - Where to create the zip
- `--output-name` - Base name for output files (optional, defaults to primary basename)

### Zip File Structure

DuoSubs creates: `/output/dir/basename.zip`

Inside the zip:
```
basename_combined.ass    ← We need this
basename_primary.ass
basename_secondary.ass
```

### Implementation Design

```go
func MergeDuoSubs(ctx context.Context, chinesePath, englishPath, outputDir string, cfg DuoSubsConfig) (string, error) {
    // 1. Determine output basename
    basename := filepath.Base(chinesePath)
    basename = strings.TrimSuffix(basename, filepath.Ext(basename))
    
    // 2. Execute DuoSubs
    cmd := exec.CommandContext(ctx, "duosubs", "merge",
        "-p", chinesePath,
        "-s", englishPath,
        "--output-dir", outputDir,
        "--output-name", basename,
        "--model", cfg.Model,
        "--device", cfg.Device,
    )
    
    if err := cmd.Run(); err != nil {
        return "", err
    }
    
    // 3. Find and extract zip
    zipPath := filepath.Join(outputDir, basename+".zip")
    
    // 4. Extract zip to outputDir
    if err := extractZip(zipPath, outputDir); err != nil {
        return "", err
    }
    
    // 5. Locate combined file
    combinedPath := filepath.Join(outputDir, basename+"_combined.ass")
    if _, err := os.Stat(combinedPath); err != nil {
        return "", fmt.Errorf("combined file not found: %s", combinedPath)
    }
    
    // 6. Clean up zip
    os.Remove(zipPath)
    
    return combinedPath, nil
}

func extractZip(zipPath, destDir string) error {
    r, err := zip.OpenReader(zipPath)
    if err != nil {
        return err
    }
    defer r.Close()
    
    for _, f := range r.File {
        // Extract each file
        outPath := filepath.Join(destDir, f.Name)
        // ... (standard zip extraction code)
    }
    
    return nil
}
```

## Key Design Decisions

### Decision 1: Primary vs Secondary Order

**Chosen:** Chinese as primary (`-p`), English as secondary (`-s`)

**Rationale:**
- DuoSubs names output files after primary
- Chinese is the "main" language for our use case (larger text on top)
- Consistent with DuoSubs conventions

### Decision 2: In-Place Extraction

**Chosen:** Extract zip contents directly to outputDir

**Alternatives:**
- Extract to subdirectory → more cleanup needed
- Keep zip, read directly → more complex

**Rationale:**
- Simpler code
- Easier cleanup (just delete zip)
- Combined file ends up in expected location

### Decision 3: Return Value Change

**Chosen:** Return actual path instead of expecting caller to guess

**Before:**
```go
func MergeDuoSubs(..., outputPath string) error
// Caller assumes output at outputPath
```

**After:**
```go
func MergeDuoSubs(..., outputDir string) (string, error)
// Returns actual path to combined file
```

**Rationale:**
- More accurate - caller gets real path
- Less fragile - no assumptions about filenames
- Better error handling - can detect if file wasn't created

## Error Handling

### Zip Not Created
```go
if _, err := os.Stat(zipPath); os.IsNotExist(err) {
    return "", fmt.Errorf("duosubs did not create expected zip: %s", zipPath)
}
```

### Zip Extraction Failure
```go
if err := extractZip(zipPath, outputDir); err != nil {
    return "", fmt.Errorf("failed to extract zip: %w", err)
}
```

### Combined File Missing
```go
if _, err := os.Stat(combinedPath); os.IsNotExist(err) {
    return "", fmt.Errorf("combined file not found in zip: %s", combinedPath)
}
```

## Testing Strategy

### Unit Tests

**Test 1: Command Construction**
```go
// Verify correct args: merge, -p, -s, --output-dir, --model, --device
```

**Test 2: Zip Extraction**
```go
// Create mock zip with combined file
// Verify extraction works
// Verify combined file found
```

**Test 3: Error Cases**
```go
// Missing zip
// Corrupt zip
// Missing combined file inside zip
```

### Integration Test

**Mock DuoSubs**
```go
// Create fake duosubs executable that creates valid zip
// Run full merger processor
// Verify combined file extracted and used
```

## Trade-offs

### Approach 1: Keep Everything Extracted (CHOSEN)

**Pros:**
- Simple cleanup (just delete zip)
- Easy to debug (files visible on disk)
- Style processor can modify combined file directly

**Cons:**
- Uses more disk space temporarily (3 files instead of 1 zip)

### Approach 2: Extract Only Combined File

**Pros:**
- Less disk usage

**Cons:**
- More complex zip reading
- Harder to debug
- Same end result anyway

**Decision:** Approach 1 - simplicity wins

## Future Enhancements

1. **Configuration**: Allow user to choose which file to use (combined/primary/secondary)
2. **Preserve Files**: Option to keep all three ASS files
3. **Custom Output Name**: Allow overriding the output basename

## References

- DuoSubs CLI help (provided in conversation)
- Go `archive/zip` documentation
- Current implementation: `internal/executor/duosubs.go`

