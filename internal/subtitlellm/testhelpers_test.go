package subtitlellm

import (
	"encoding/json"
	"os"
)

// decodeJSON and prototype event loading are shared test helpers used by the
// render tests; they live here so alignment-owned tests stay self-contained.

func decodeJSON(data []byte, value any) error {
	return json.Unmarshal(data, value)
}

type prototypeEvent struct {
	Group     int        `json:"group"`
	Strategy  string     `json:"strategy"`
	ENIDs     []int      `json:"en_ids"`
	ZHIDs     []int      `json:"zh_ids"`
	ASSStart  int        `json:"ass_start"`
	ASSEnd    int        `json:"ass_end"`
	Alignment int        `json:"alignment"`
	Position  [2]float64 `json:"position_used"`
	Band      [2]float64 `json:"band"`
}

func readPrototypeEvents(path string) ([]prototypeEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var events []prototypeEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, err
	}
	return events, nil
}
