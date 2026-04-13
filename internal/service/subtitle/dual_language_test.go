package subtitle

import (
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
