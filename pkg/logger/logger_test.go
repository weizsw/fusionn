package logger

import (
	"testing"
)

func TestInit(t *testing.T) {
	tests := []struct {
		name  string
		isDev bool
	}{
		{
			name:  "development mode",
			isDev: true,
		},
		{
			name:  "production mode",
			isDev: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Init(tt.isDev)
			if Log == nil {
				t.Fatal("Logger was not initialized")
			}
			defer Sync()
		})
	}
}

func TestLogMethods(t *testing.T) {
	Init(true)
	defer Sync()

	// Test that these don't panic
	Info("test info")
	Infof("test info: %s", "formatted")
	Debug("test debug")
	Debugf("test debug: %s", "formatted")
	Warn("test warn")
	Warnf("test warn: %s", "formatted")
	Error("test error")
	Errorf("test error: %s", "formatted")
}

func TestIndentLogger(t *testing.T) {
	Init(true)
	defer Sync()

	// Test indented logger methods don't panic
	log := Indent()
	log.Info("test indented info")
	log.Infof("test indented info: %s", "formatted")
	log.Debug("test indented debug")
	log.Debugf("test indented debug: %s", "formatted")
	log.Warn("test indented warn")
	log.Warnf("test indented warn: %s", "formatted")
	log.Error("test indented error")
	log.Errorf("test indented error: %s", "formatted")
}

func TestIndentPrefix(t *testing.T) {
	expected := "  ├─ "
	if IndentPrefix != expected {
		t.Errorf("IndentPrefix = %q, want %q", IndentPrefix, expected)
	}
}
