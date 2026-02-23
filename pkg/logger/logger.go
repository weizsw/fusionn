package logger

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.SugaredLogger

const IndentPrefix = "  ├─ "

func Init(isDev bool) {
	var encoder zapcore.Encoder
	var level zapcore.Level

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:       "time",
		LevelKey:      "level",
		MessageKey:    "msg",
		StacktraceKey: "", // Hide stacktrace in normal logs
		EncodeTime:    customTimeEncoder,
		EncodeCaller:  nil, // Hide caller
	}

	if isDev {
		// Development: colorful console output
		level = zapcore.DebugLevel
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoderConfig.ConsoleSeparator = " "
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		// Production: clean console output (no JSON)
		level = zapcore.InfoLevel
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		encoderConfig.ConsoleSeparator = " "
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(os.Stdout),
		level,
	)

	logger := zap.New(core)
	Log = logger.Sugar()
}

// customTimeEncoder formats time as "2006-01-02 15:04:05" for logs
func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05"))
}

func Sync() {
	if Log != nil {
		_ = Log.Sync() //nolint:errcheck // Sync can fail on stdout/stderr, safe to ignore
	}
}

// Convenience methods
func Info(args ...interface{})                    { Log.Info(args...) }
func Infof(template string, args ...interface{})  { Log.Infof(template, args...) }
func Error(args ...interface{})                   { Log.Error(args...) }
func Errorf(template string, args ...interface{}) { Log.Errorf(template, args...) }
func Debug(args ...interface{})                   { Log.Debug(args...) }
func Debugf(template string, args ...interface{}) { Log.Debugf(template, args...) }
func Warn(args ...interface{})                    { Log.Warn(args...) }
func Warnf(template string, args ...interface{})  { Log.Warnf(template, args...) }
func Fatal(args ...interface{})                   { Log.Fatal(args...); os.Exit(1) }
func Fatalf(template string, args ...interface{}) { Log.Fatalf(template, args...); os.Exit(1) }

// Indent returns an indented logger that prefixes all log messages with IndentPrefix.
// Use this within processors to create visual hierarchy for sub-logs.
func Indent() IndentedLogger {
	return IndentedLogger{prefix: IndentPrefix}
}

// IndentedLogger wraps logging methods with an indent prefix.
type IndentedLogger struct {
	prefix string
}

func (il IndentedLogger) Info(args ...interface{}) {
	Log.Info(il.prefix, fmt.Sprint(args...))
}

func (il IndentedLogger) Infof(template string, args ...interface{}) {
	Log.Infof(il.prefix+template, args...)
}

func (il IndentedLogger) Error(args ...interface{}) {
	Log.Error(il.prefix, fmt.Sprint(args...))
}

func (il IndentedLogger) Errorf(template string, args ...interface{}) {
	Log.Errorf(il.prefix+template, args...)
}

func (il IndentedLogger) Debug(args ...interface{}) {
	Log.Debug(il.prefix, fmt.Sprint(args...))
}

func (il IndentedLogger) Debugf(template string, args ...interface{}) {
	Log.Debugf(il.prefix+template, args...)
}

func (il IndentedLogger) Warn(args ...interface{}) {
	Log.Warn(il.prefix, fmt.Sprint(args...))
}

func (il IndentedLogger) Warnf(template string, args ...interface{}) {
	Log.Warnf(il.prefix+template, args...)
}

// InfoWriter returns an io.Writer that writes subprocess output to stderr.
// Lines are prefixed with │ to visually distinguish from service logs.
type InfoWriter struct {
	prefix string
	buffer []byte
}

func NewInfoWriter() *InfoWriter {
	return &InfoWriter{
		prefix: "  │ ",
		buffer: make([]byte, 0, 1024),
	}
}

func (w *InfoWriter) Write(p []byte) (n int, err error) {
	w.buffer = append(w.buffer, p...)

	// Process complete lines (split on \n or \r for progress bars)
	for {
		idx := bytes.IndexByte(w.buffer, '\n')
		if idx == -1 {
			idx = bytes.IndexByte(w.buffer, '\r')
			if idx == -1 {
				break
			}
		}

		line := string(w.buffer[:idx])
		if line != "" {
			// Write directly to stderr with prefix, then flush
			fmt.Fprintf(os.Stderr, "%s%s\n", w.prefix, line)
		}
		w.buffer = w.buffer[idx+1:]
	}

	return len(p), nil
}
