package logger

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	L    *zap.Logger
	S    *zap.SugaredLogger
	once sync.Once
)

func init() {
	once.Do(func() {
		conf := zap.NewDevelopmentConfig()
		if os.Getenv("GIN_MODE") == "release" {
			conf = zap.NewProductionConfig()
		}

		// Customize the encoder config
		conf.EncoderConfig = zapcore.EncoderConfig{
			TimeKey:          "time",
			LevelKey:         "level",
			NameKey:          "logger",
			MessageKey:       "msg",
			StacktraceKey:    "stacktrace",
			LineEnding:       zapcore.DefaultLineEnding,
			EncodeLevel:      customLevelEncoder,
			EncodeTime:       customTimeEncoder,
			EncodeName:       zapcore.FullNameEncoder,
			ConsoleSeparator: " ",
		}

		// Use console encoder instead of JSON
		conf.Encoding = "console"

		logger, err := conf.Build()
		if err != nil {
			panic(err)
		}
		L = logger
		S = logger.Sugar()

		// Replace Gin's default logger
		gin.DefaultWriter = &zapWriter{sugar: S}
	})
}

func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString("[" + t.Format("2006-01-02 15:04:05") + "]")
}

func customLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString("[" + strings.ToUpper(l.String()) + "]")
}

// zapWriter adapts zap logger to gin's writer interface
type zapWriter struct {
	sugar *zap.SugaredLogger
}

func (w *zapWriter) Write(p []byte) (n int, err error) {
	w.sugar.Info(string(p))
	return len(p), nil
}
