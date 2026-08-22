package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envVars is every variable Load reads. Tests clear all of them so a
// value inherited from the developer's shell cannot make a case pass or
// fail by accident.
var envVars = []string{
	"PORT", "TRUSTED_PROXIES",
	"READ_HEADER_TIMEOUT", "READ_TIMEOUT", "WRITE_TIMEOUT", "IDLE_TIMEOUT",
	"SHUTDOWN_TIMEOUT", "REQUEST_TIMEOUT",
	"MAX_BODY_BYTES", "LOG_LEVEL", "LOG_FORMAT",
}

// setEnv clears every known variable, then applies the given overrides.
// t.Setenv restores the previous values, and forbids t.Parallel.
func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()

	for _, k := range envVars {
		t.Setenv(k, "")
	}
	for k, v := range overrides {
		t.Setenv(k, v)
	}
}

func TestLoad_Defaults(t *testing.T) {
	setEnv(t, nil)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, DefaultPort, cfg.Port)
	assert.Nil(t, cfg.TrustedProxies, "默认不信任任何代理")
	assert.Equal(t, DefaultReadHeaderTimeout, cfg.ReadHeaderTimeout)
	assert.Equal(t, DefaultReadTimeout, cfg.ReadTimeout)
	assert.Equal(t, DefaultWriteTimeout, cfg.WriteTimeout)
	assert.Equal(t, DefaultIdleTimeout, cfg.IdleTimeout)
	assert.Equal(t, DefaultShutdownTimeout, cfg.ShutdownTimeout)
	assert.Equal(t, DefaultRequestTimeout, cfg.RequestTimeout)
	assert.Equal(t, DefaultMaxBodyBytes, cfg.MaxBodyBytes)
	assert.Equal(t, DefaultLogLevel, cfg.LogLevel)
	assert.Equal(t, DefaultLogFormat, cfg.LogFormat)
}

func TestLoad_Overrides(t *testing.T) {
	setEnv(t, map[string]string{
		"PORT":                "9000",
		"TRUSTED_PROXIES":     "10.0.0.1, 192.168.0.0/16 ,,  172.16.0.0/12",
		"READ_HEADER_TIMEOUT": "2s",
		"READ_TIMEOUT":        "20s",
		"WRITE_TIMEOUT":       "25s",
		"IDLE_TIMEOUT":        "2m",
		"SHUTDOWN_TIMEOUT":    "30s",
		"REQUEST_TIMEOUT":     "20s",
		"MAX_BODY_BYTES":      "2048",
		"LOG_LEVEL":           "debug",
		"LOG_FORMAT":          "text",
	})

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "9000", cfg.Port)
	assert.Equal(t, ":9000", cfg.Addr())
	// Blank entries dropped, surrounding spaces trimmed.
	assert.Equal(t, []string{"10.0.0.1", "192.168.0.0/16", "172.16.0.0/12"}, cfg.TrustedProxies)
	assert.Equal(t, 2*time.Second, cfg.ReadHeaderTimeout)
	assert.Equal(t, 20*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 25*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 2*time.Minute, cfg.IdleTimeout)
	assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, 20*time.Second, cfg.RequestTimeout)
	assert.Equal(t, int64(2048), cfg.MaxBodyBytes)
	assert.Equal(t, slog.LevelDebug, cfg.LogLevel)
	assert.Equal(t, FormatText, cfg.LogFormat)
}

// TestLoad_EmptyMeansUnset pins that an explicitly empty variable falls
// back to the default rather than producing an empty setting, so a stray
// `PORT=` in a compose file is not a silent misconfiguration.
func TestLoad_EmptyMeansUnset(t *testing.T) {
	setEnv(t, map[string]string{"PORT": "", "LOG_LEVEL": "  "})

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, DefaultPort, cfg.Port)
	assert.Equal(t, DefaultLogLevel, cfg.LogLevel)
}

func TestLoad_InvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "端口不是数字",
			env:     map[string]string{"PORT": "abc"},
			wantErr: `PORT must be a number, got "abc"`,
		},
		{
			name:    "端口为零",
			env:     map[string]string{"PORT": "0"},
			wantErr: "PORT must be between 1 and 65535",
		},
		{
			name:    "端口超出范围",
			env:     map[string]string{"PORT": "70000"},
			wantErr: "PORT must be between 1 and 65535",
		},
		{
			name:    "时长格式错误",
			env:     map[string]string{"REQUEST_TIMEOUT": "5x"},
			wantErr: `REQUEST_TIMEOUT must be a duration such as "5s", got "5x"`,
		},
		{
			name:    "时长为负",
			env:     map[string]string{"READ_TIMEOUT": "-3s"},
			wantErr: "READ_TIMEOUT must be positive",
		},
		{
			name:    "时长为零",
			env:     map[string]string{"IDLE_TIMEOUT": "0s"},
			wantErr: "IDLE_TIMEOUT must be positive",
		},
		{
			name:    "体积上限不是整数",
			env:     map[string]string{"MAX_BODY_BYTES": "1MiB"},
			wantErr: `MAX_BODY_BYTES must be an integer, got "1MiB"`,
		},
		{
			name:    "体积上限为零",
			env:     map[string]string{"MAX_BODY_BYTES": "0"},
			wantErr: "MAX_BODY_BYTES must be positive",
		},
		{
			name:    "日志级别不认识",
			env:     map[string]string{"LOG_LEVEL": "trace"},
			wantErr: `LOG_LEVEL must be debug, info, warn or error, got "trace"`,
		},
		{
			name:    "日志格式不支持",
			env:     map[string]string{"LOG_FORMAT": "xml"},
			wantErr: `LOG_FORMAT must be "json" or "text", got "xml"`,
		},
		{
			name:    "可信代理不是 IP 或 CIDR",
			env:     map[string]string{"TRUSTED_PROXIES": "10.0.0.1,not-an-ip"},
			wantErr: `TRUSTED_PROXIES: "not-an-ip" is neither an IP nor a CIDR`,
		},
		{
			name: "请求超时不短于写超时",
			env: map[string]string{
				"REQUEST_TIMEOUT": "10s",
				"WRITE_TIMEOUT":   "10s",
			},
			wantErr: "must be shorter than WRITE_TIMEOUT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)

			cfg, err := Load()

			require.Error(t, err, "这个值应该被拒绝")
			assert.Nil(t, cfg, "出错时不应返回半成品配置")
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestLoad_ReportsEveryProblem covers the choice to collect all errors
// instead of failing on the first, so a misconfigured deployment can be
// fixed in one pass rather than one restart per typo.
func TestLoad_ReportsEveryProblem(t *testing.T) {
	setEnv(t, map[string]string{
		"PORT":            "abc",
		"LOG_FORMAT":      "xml",
		"REQUEST_TIMEOUT": "5x",
	})

	_, err := Load()
	require.Error(t, err)

	got := err.Error()
	for _, want := range []string{"PORT", "LOG_FORMAT", "REQUEST_TIMEOUT"} {
		assert.Contains(t, got, want, "三个问题应该一次全部报出")
	}
	assert.Equal(t, 3, strings.Count(got, "\n")+1, "应恰好三条错误")
}

func TestLogValueRedactsNothingYet(t *testing.T) {
	setEnv(t, map[string]string{"PORT": "9000"})

	cfg, err := Load()
	require.NoError(t, err)

	attrs := cfg.LogValue()
	require.Equal(t, slog.KindGroup, attrs.Kind())

	found := map[string]bool{}
	for _, a := range attrs.Group() {
		found[a.Key] = true
	}

	// Guards against a field being added to Config but forgotten here.
	for _, key := range []string{
		"port", "trusted_proxies", "read_header_timeout", "read_timeout",
		"write_timeout", "idle_timeout", "shutdown_timeout", "request_timeout",
		"max_body_bytes", "log_level", "log_format",
	} {
		assert.True(t, found[key], "LogValue 缺少字段 %q", key)
	}
}
