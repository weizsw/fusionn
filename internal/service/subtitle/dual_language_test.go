package subtitle

import (
	"strings"
	"testing"
)

func TestContainsCJK(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"chinese characters", "你好世界", true},
		{"english only", "Hello World", false},
		{"mixed line", "你好 Hello", true},
		{"empty string", "", false},
		{"numbers only", "12345", false},
		{"cjk extension a", "㐀㐁", true},
		{"punctuation only", "...,,,!!!", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsCJK(tt.text); got != tt.want {
				t.Errorf("containsCJK(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestContainsLatin(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"english sentence", "Hello World", true},
		{"chinese only", "你好世界", false},
		{"mixed line", "你好 Hello", true},
		{"empty string", "", false},
		{"numbers only", "12345", false},
		{"single letter", "a", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsLatin(tt.text); got != tt.want {
				t.Errorf("containsLatin(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestClassifyLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want lineLanguage
	}{
		{"chinese", "你好世界", langChinese},
		{"english", "Hello World", langEnglish},
		{"mixed", "你好 Hello World", langMixed},
		{"empty", "", langUnknown},
		{"numbers", "12345", langUnknown},
		{"chinese with punctuation", "你好！世界。", langChinese},
		{"english with numbers", "Episode 5", langEnglish},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyLine(tt.line); got != tt.want {
				t.Errorf("classifyLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestParseSRTCues(t *testing.T) {
	input := "1\n00:00:01,000 --> 00:00:03,000\n你好世界\nHello World\n\n2\n00:00:04,000 --> 00:00:06,000\n再见\nGoodbye\n\n"

	cues, err := parseSRTCues(input)
	if err != nil {
		t.Fatalf("parseSRTCues() error = %v", err)
	}
	if len(cues) != 2 {
		t.Fatalf("parseSRTCues() got %d cues, want 2", len(cues))
	}
	if cues[0].startTime != "00:00:01,000" || cues[0].endTime != "00:00:03,000" {
		t.Errorf("cue[0] times = %q-%q, want 00:00:01,000-00:00:03,000", cues[0].startTime, cues[0].endTime)
	}
	if len(cues[0].lines) != 2 {
		t.Errorf("cue[0] lines = %d, want 2", len(cues[0].lines))
	}
}

func TestDetectDualLanguageSRT(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "dual language alternating lines",
			content: "1\n00:00:01,000 --> 00:00:03,000\n你好世界\nHello World\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\n再见\nGoodbye\n\n" +
				"3\n00:00:07,000 --> 00:00:09,000\n谢谢\nThank you\n\n",
			want: true,
		},
		{
			name: "dual language with backslash N",
			content: "1\n00:00:01,000 --> 00:00:03,000\n你好世界\\NHello World\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\n再见\\NGoodbye\n\n",
			want: true,
		},
		{
			name: "chinese only",
			content: "1\n00:00:01,000 --> 00:00:03,000\n你好世界\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\n再见\n\n",
			want: false,
		},
		{
			name: "english only",
			content: "1\n00:00:01,000 --> 00:00:03,000\nHello World\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\nGoodbye\n\n",
			want: false,
		},
		{
			name: "mostly dual with some chinese only",
			content: "1\n00:00:01,000 --> 00:00:03,000\n你好世界\nHello World\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\n再见\nGoodbye\n\n" +
				"3\n00:00:07,000 --> 00:00:09,000\n谢谢\n\n" +
				"4\n00:00:10,000 --> 00:00:12,000\n对不起\nSorry\n\n",
			want: true,
		},
		{
			name: "single line mixed characters",
			content: "1\n00:00:01,000 --> 00:00:03,000\n你好世界 Hello World\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\n再见 Goodbye\n\n",
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectDualLanguageSRT(tt.content); got != tt.want {
				t.Errorf("detectDualLanguageSRT() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectDualLanguageASS(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "dual language with two styles",
			content: `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour
Style: Chinese,Arial,20,&H00FFFFFF
Style: English,Arial,16,&H00FFFF00

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Chinese,,0,0,0,,你好世界
Dialogue: 0,0:00:01.00,0:00:03.00,English,,0,0,0,,Hello World
Dialogue: 0,0:00:04.00,0:00:06.00,Chinese,,0,0,0,,再见
Dialogue: 0,0:00:04.00,0:00:06.00,English,,0,0,0,,Goodbye
`,
			want: true,
		},
		{
			name: "chinese only ASS",
			content: `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour
Style: Default,Arial,20,&H00FFFFFF

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,你好世界
Dialogue: 0,0:00:04.00,0:00:06.00,Default,,0,0,0,,再见
`,
			want: false,
		},
		{
			name: "bilingual text in single style with backslash N",
			content: `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour
Style: Default,Arial,20,&H00FFFFFF

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,你好世界\NHello World
Dialogue: 0,0:00:04.00,0:00:06.00,Default,,0,0,0,,再见\NGoodbye
`,
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectDualLanguageASS(tt.content); got != tt.want {
				t.Errorf("detectDualLanguageASS() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Suppress unused import warning — strings used in later tasks
var _ = strings.Contains
