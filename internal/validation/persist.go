package validation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// persistResult writes the pipeline result for a turn to
// validationDir/<turn_index>.json (SPEC §15.2 step 5). The write is atomic: it
// marshals to a sibling temp file and renames into place so a crash mid-write
// never leaves a torn JSON file.
func persistResult(validationDir string, res ValidationPipelineResult) error {
	if validationDir == "" {
		return fmt.Errorf("validation: persist: empty validation dir: %w", ErrPersist)
	}
	if err := os.MkdirAll(validationDir, 0o755); err != nil {
		return fmt.Errorf("validation: persist: mkdir %q: %w", validationDir, err)
	}

	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("validation: persist: marshal turn %d: %w", res.TurnIndex, err)
	}

	finalPath := turnResultPath(validationDir, res.TurnIndex)
	tmp, err := os.CreateTemp(validationDir, fmt.Sprintf(".%d-*.json.tmp", res.TurnIndex))
	if err != nil {
		return fmt.Errorf("validation: persist: temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("validation: persist: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("validation: persist: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("validation: persist: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("validation: persist: rename to %q: %w", finalPath, err)
	}
	return nil
}

// turnResultPath returns the JSON path for a turn index within validationDir.
func turnResultPath(validationDir string, turnIndex int) string {
	return filepath.Join(validationDir, strconv.Itoa(turnIndex)+".json")
}

// LoadResult reads a previously persisted per-turn result. It is the read path
// the Phase 7 router uses to fetch the previous turn's results for context
// injection.
func LoadResult(validationDir string, turnIndex int) (ValidationPipelineResult, error) {
	var res ValidationPipelineResult
	data, err := os.ReadFile(turnResultPath(validationDir, turnIndex))
	if err != nil {
		return res, fmt.Errorf("validation: load turn %d: %w", turnIndex, err)
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return res, fmt.Errorf("validation: load turn %d: unmarshal: %w", turnIndex, err)
	}
	return res, nil
}
