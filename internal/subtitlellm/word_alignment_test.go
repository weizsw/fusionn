package subtitlellm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/fusionn/internal/config"
)

func TestWordAlignmentUploadsAudioAndRestoresVideoClock(t *testing.T) {
	const text = "The first part and then the second part."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/align" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer func() { _ = file.Close() }()
		body, _ := io.ReadAll(file)
		if string(body) != "WAV audio bytes" || header.Filename != "clip.wav" || r.FormValue("text") != text || r.FormValue("language") != "English" {
			t.Error("audio upload or source transcript changed")
		}
		_, _ = io.WriteString(w, `{"words":[{"text":"The","start_time":0.3,"end_time":0.5}]}`)
	}))
	defer server.Close()
	bundle := Bundle{EN: []Cue{
		{ID: 1, Start: 1000, End: 6000, Display: text},
		{ID: 2, Start: 7000, End: 10000, Display: "Short."},
		{ID: 3, Start: 11000, End: 15000, Display: text, MultiSpeaker: true},
	}}
	extractions := 0
	words, err := collectWordTimings(context.Background(), bundle, config.LLMConfig{EnChars: 22, WordAlignURL: server.URL}, func(ctx context.Context, cue Cue) ([]byte, int, error) {
		extractions++
		if cue.ID != 1 {
			t.Errorf("extracted an unnecessary cue: %d", cue.ID)
		}
		return []byte("WAV audio bytes"), 700, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := WordTimings{1: {{Text: "The", StartMS: 1000, EndMS: 1200}}}
	if !reflect.DeepEqual(words, want) || extractions != 1 {
		t.Fatalf("words = %+v, extractions = %d", words, extractions)
	}
}

func TestWordAlignmentKeepsCompletedCuesOnServiceFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_, _ = io.WriteString(w, `{"words":[{"text":"The","start_time":0.3,"end_time":0.5}]}`)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	bundle := Bundle{}
	for i := 1; i <= 3; i++ {
		bundle.EN = append(bundle.EN, Cue{ID: i, Start: i * 10000, End: i*10000 + 5000, Display: "The first part and then the second part."})
	}
	words, err := collectWordTimings(context.Background(), bundle, config.LLMConfig{EnChars: 22, WordAlignURL: server.URL}, func(ctx context.Context, cue Cue) ([]byte, int, error) {
		return []byte("WAV"), cue.Start - 300, nil
	})
	if err == nil || len(words) != 1 || len(words[1]) != 1 || requests != 2 {
		t.Fatalf("completed cues or early stop lost: words=%+v, requests=%d, error=%v", words, requests, err)
	}
}

func TestWordAlignmentCancellationAndDisabledPath(t *testing.T) {
	bundle := Bundle{EN: []Cue{{ID: 1, Start: 1000, End: 7000, Display: "A long sentence that needs a second fragment."}}}
	cfg := config.LLMConfig{EnChars: 22}
	if words, err := AlignWords(context.Background(), "/no/video/needed.mkv", bundle, cfg); err != nil || len(words) != 0 {
		t.Fatalf("disabled service extracted audio: words=%v, error=%v", words, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg.WordAlignURL = "http://unused.invalid"
	if _, err := AlignWords(ctx, "/no/video/needed.mkv", bundle, cfg); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation became a fallback: %v", err)
	}
}

func TestWordAlignmentRejectsInvalidResponseTimes(t *testing.T) {
	for _, payload := range []any{
		map[string]any{"text": "word", "start_time": -1, "end_time": 1},
		map[string]any{"text": "word", "start_time": 2, "end_time": 1},
		map[string]any{"text": "word", "start_time": 1, "end_time": 99},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"words": []any{payload}})
		}))
		_, err := requestWordTimings(context.Background(), http.DefaultClient, server.URL, "word", []byte("WAV"), 0)
		server.Close()
		if err == nil {
			t.Fatalf("accepted invalid model times: %v", payload)
		}
	}
}

func TestAudioCenterChannelSelection(t *testing.T) {
	for _, layout := range []string{"5.1(side)", "7.1(wide)", "3.0", "5 channels (FL+FR+FC+BL+BR)"} {
		if !hasCenterChannel(layout) {
			t.Errorf("center channel not detected: %s", layout)
		}
	}
	for _, layout := range []string{"stereo", "quad", "3.0(back)", "6.0(front)", "6.1(front)", ""} {
		if hasCenterChannel(layout) {
			t.Errorf("missing center channel was selected: %s", layout)
		}
	}
}
