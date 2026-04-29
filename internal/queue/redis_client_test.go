package queue

import (
	"encoding/json"
	"testing"
)

func TestTranslationJobMarshalIncludesGlossaryIdentity(t *testing.T) {
	job := TranslationJob{
		JobID:        "job-1",
		VideoPath:    "/tv/The Capture/Season 01/S01E01.mkv",
		SubtitlePath: "/tv/The Capture/Season 01/S01E01.eng.srt",
		MediaType:    "episode",
		MediaTitle:   "The Capture S01E01",
		SourceSystem: "sonarr",
		MediaID:      "42",
		ExternalIDs: map[string]string{
			"sonarr": "42",
			"tvdb":   "355620",
			"imdb":   "tt8201186",
		},
		Season:  1,
		Episode: 1,
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if payload["source_system"] != "sonarr" {
		t.Fatalf("source_system = %v", payload["source_system"])
	}
	if payload["media_id"] != "42" {
		t.Fatalf("media_id = %v", payload["media_id"])
	}
	externalIDs, ok := payload["external_ids"].(map[string]any)
	if !ok {
		t.Fatalf("external_ids missing or wrong type: %T", payload["external_ids"])
	}
	if externalIDs["tvdb"] != "355620" {
		t.Fatalf("external_ids.tvdb = %v", externalIDs["tvdb"])
	}
	if payload["season"] != float64(1) || payload["episode"] != float64(1) {
		t.Fatalf("season/episode = %v/%v", payload["season"], payload["episode"])
	}
}

func TestTranslationJobMarshalOmitsEmptyGlossaryIdentity(t *testing.T) {
	job := TranslationJob{
		JobID:        "job-1",
		VideoPath:    "/movies/Movie.mkv",
		SubtitlePath: "/movies/Movie.eng.srt",
		MediaType:    "movie",
		MediaTitle:   "Movie",
	}

	data, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"source_system", "media_id", "external_ids", "season", "episode"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("expected %s to be omitted from %#v", key, payload)
		}
	}
}
