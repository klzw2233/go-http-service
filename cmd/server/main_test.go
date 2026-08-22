package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/config"
)

// TestNewLogger covers the only real logic left in package main after
// PORT parsing moved to the config package.
func TestNewLogger(t *testing.T) {
	tests := []struct {
		name   string
		format string
		level  slog.Level
		// emit is logged at info level; wantEmitted says whether the
		// configured level lets it through.
		wantEmitted bool
		wantJSON    bool
	}{
		{
			name:        "JSON 格式",
			format:      config.FormatJSON,
			level:       slog.LevelInfo,
			wantEmitted: true,
			wantJSON:    true,
		},
		{
			name:        "text 格式",
			format:      config.FormatText,
			level:       slog.LevelInfo,
			wantEmitted: true,
			wantJSON:    false,
		},
		{
			name:        "debug 级别放行 info",
			format:      config.FormatJSON,
			level:       slog.LevelDebug,
			wantEmitted: true,
			wantJSON:    true,
		},
		{
			name:        "error 级别过滤掉 info",
			format:      config.FormatJSON,
			level:       slog.LevelError,
			wantEmitted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cfg := &config.Config{LogFormat: tt.format, LogLevel: tt.level}

			newLogger(cfg, &buf).Info("hello", "key", "value")

			out := buf.String()
			if !tt.wantEmitted {
				assert.Empty(t, out, "该级别不应输出 info 日志")
				return
			}

			require.NotEmpty(t, out)
			assert.Contains(t, out, "hello")
			assert.Contains(t, out, "value")

			if tt.wantJSON {
				var record map[string]any
				require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &record),
					"JSON 格式的输出应该能被解析: %s", out)
				assert.Equal(t, "hello", record["msg"])
				assert.Equal(t, "value", record["key"])
			} else {
				// text handler 用 key=value，不是 JSON
				assert.Contains(t, out, "key=value")
				assert.False(t, json.Valid([]byte(out)), "text 格式不应是合法 JSON")
			}
		})
	}
}

// TestNewLoggerDefaultsToJSON pins that an unrecognised format falls back
// to JSON rather than producing no logger. config.Load rejects bad values
// before this point, so this only guards direct construction.
func TestNewLoggerDefaultsToJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := &config.Config{LogFormat: "something-else", LogLevel: slog.LevelInfo}

	newLogger(cfg, &buf).Info("hello")

	assert.True(t, json.Valid([]byte(strings.TrimSpace(buf.String()))))
}
