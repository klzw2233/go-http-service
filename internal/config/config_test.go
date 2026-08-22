package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"reflect"
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
	"DATABASE_URL", "DB_MAX_CONNS", "DB_CONNECT_TIMEOUT",
	"RATE_LIMIT_RPS", "RATE_LIMIT_BURST",
	"LOGIN_RATE_LIMIT_RPM", "LOGIN_RATE_LIMIT_BURST",
}

// testDSN is a syntactically valid connection string. It is seeded by
// setEnv because DATABASE_URL is required, so a test that does not care
// about the database still needs one to reach the assertions it does
// care about.
const testDSN = "postgres://app:pw@db:5432/svc"

// setEnv clears every known variable, seeds the required ones, then
// applies the given overrides. t.Setenv restores the previous values,
// and forbids t.Parallel.
//
// Pass DATABASE_URL: "" in overrides to test the missing-database case.
func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()

	for _, k := range envVars {
		t.Setenv(k, "")
	}
	t.Setenv("DATABASE_URL", testDSN)

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
	assert.Equal(t, testDSN, cfg.DatabaseURL, "DATABASE_URL 现在是必需项，由 setEnv 注入")
	assert.Equal(t, DefaultDBMaxConns, cfg.DBMaxConns)
	assert.Equal(t, DefaultDBConnectTimeout, cfg.DBConnectTimeout)
	assert.Equal(t, DefaultRateLimitRPS, cfg.RateLimitRPS)
	assert.Equal(t, DefaultRateLimitBurst, cfg.RateLimitBurst)
	assert.Equal(t, DefaultLoginRateLimitRPM, cfg.LoginRateLimitRPM)
	assert.Equal(t, DefaultLoginRateLimitBurst, cfg.LoginRateLimitBurst)
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
		"DATABASE_URL":        "postgres://app:pw@db:5432/svc",
		"DB_MAX_CONNS":        "25",
		"DB_CONNECT_TIMEOUT":  "3s",
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
	assert.Equal(t, "postgres://app:pw@db:5432/svc", cfg.DatabaseURL)
	assert.Equal(t, int64(25), cfg.DBMaxConns)
	assert.Equal(t, 3*time.Second, cfg.DBConnectTimeout)
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
		{
			name: "配了库但连接数为零",
			env: map[string]string{
				"DATABASE_URL": "postgres://app:pw@db:5432/svc",
				"DB_MAX_CONNS": "0",
			},
			wantErr: "DB_MAX_CONNS must be positive",
		},
		{
			name:    "建连超时为负",
			env:     map[string]string{"DB_CONNECT_TIMEOUT": "-1s"},
			wantErr: "DB_CONNECT_TIMEOUT must be positive",
		},
		{
			// Required since the service gained endpoints that read and
			// write the database; it was optional while none did.
			name:    "缺少数据库连接串",
			env:     map[string]string{"DATABASE_URL": ""},
			wantErr: "DATABASE_URL is required",
		},
		{
			// A non-positive budget locks every caller out rather than
			// slowing abusers down, which is never what was meant.
			name:    "全局限流为零",
			env:     map[string]string{"RATE_LIMIT_RPS": "0"},
			wantErr: "RATE_LIMIT_RPS must be positive",
		},
		{
			name:    "登录限流为负",
			env:     map[string]string{"LOGIN_RATE_LIMIT_RPM": "-1"},
			wantErr: "LOGIN_RATE_LIMIT_RPM must be positive",
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

// TestLogValueCoversEveryField fails when a field is added to Config but
// not to LogValue, which would make the startup record quietly
// incomplete. Comparing counts rather than names avoids inventing a
// CamelCase-to-snake_case rule that has to special-case DB and URL.
func TestLogValueCoversEveryField(t *testing.T) {
	setEnv(t, map[string]string{"PORT": "9000"})

	cfg, err := Load()
	require.NoError(t, err)

	value := cfg.LogValue()
	require.Equal(t, slog.KindGroup, value.Kind())

	logged := value.Group()
	fields := reflect.TypeOf(Config{}).NumField()

	require.Len(t, logged, fields,
		"Config 有 %d 个字段但 LogValue 输出了 %d 个，新增字段忘了同步？", fields, len(logged))

	// Keys must be unique; a copy-paste slip would otherwise hide a field.
	seen := make(map[string]bool, len(logged))
	for _, attr := range logged {
		assert.NotEmpty(t, attr.Key)
		assert.False(t, seen[attr.Key], "字段 %q 重复", attr.Key)
		seen[attr.Key] = true
	}
}

// TestLogValueRendersDurationsReadably pins the choice of strings over
// slog.Duration. A JSON handler renders slog.Duration as a nanosecond
// integer, and "read_header_timeout":5000000000 does not tell a human
// scanning the startup log whether the deployment picked up the right
// timeout.
func TestLogValueRendersDurationsReadably(t *testing.T) {
	setEnv(t, map[string]string{
		"READ_HEADER_TIMEOUT": "5s",
		"REQUEST_TIMEOUT":     "1500ms",
		"IDLE_TIMEOUT":        "2m",
	})

	cfg, err := Load()
	require.NoError(t, err)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("startup", "config", cfg)

	var record struct {
		Config map[string]any `json:"config"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record),
		"日志不是合法 JSON: %s", buf.String())

	assert.Equal(t, "5s", record.Config["read_header_timeout"])
	assert.Equal(t, "1.5s", record.Config["request_timeout"])
	assert.Equal(t, "2m0s", record.Config["idle_timeout"])
	assert.Equal(t, DefaultWriteTimeout.String(), record.Config["write_timeout"])

	// Non-duration fields keep their natural types.
	assert.Equal(t, float64(DefaultMaxBodyBytes), record.Config["max_body_bytes"])
	assert.Equal(t, DefaultPort, record.Config["port"])
}

// dsnPassword is the credential every redaction test plants, so an
// assertion that it did not appear is checking something real.
const dsnPassword = "sup3rs3cr3t"

// TestLogValueRedactsDatabasePassword is the one test in this package
// that guards a credential rather than a behaviour.
//
// LogValue is written on every start and kept by whatever collects the
// logs, so a raw DSN here means the database password is on disk in
// perpetuity.
func TestLogValueRedactsDatabasePassword(t *testing.T) {
	dsn := "postgres://app:" + dsnPassword + "@db.internal:5432/svc?sslmode=disable"
	setEnv(t, map[string]string{"DATABASE_URL": dsn})

	cfg, err := Load()
	require.NoError(t, err)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("startup", "config", cfg)
	logged := buf.String()

	assert.NotContains(t, logged, dsnPassword, "启动日志泄露了数据库密码: %s", logged)

	var record struct {
		Config map[string]any `json:"config"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))

	got, _ := record.Config["database_url"].(string)
	assert.Contains(t, got, "xxxxx", "密码应被替换为 xxxxx")
	// Host and database survive: that is what makes the record useful for
	// confirming the deployment connected where it was meant to.
	assert.Contains(t, got, "db.internal:5432")
	assert.Contains(t, got, "svc")
}

func TestRedactDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "未设置",
			dsn:  "",
			want: dsnUnset,
		},
		{
			name: "标准 URL 形式",
			dsn:  "postgres://app:" + dsnPassword + "@db:5432/svc",
			want: "postgres://app:xxxxx@db:5432/svc",
		},
		{
			name: "无密码",
			dsn:  "postgres://app@db:5432/svc",
			want: "postgres://app@db:5432/svc",
		},
		{
			name: "无用户信息",
			dsn:  "postgres://db:5432/svc",
			want: "postgres://db:5432/svc",
		},
		{
			// pgx also accepts the keyword/value form, which url.Parse
			// reads as a bare path with no scheme. Returning a fixed
			// placeholder is the only safe answer: the string contains a
			// password and there is no structure to redact.
			name: "keyword/value 形式",
			dsn:  "host=db port=5432 user=app password=" + dsnPassword,
			want: dsnOpaque,
		},
		{
			name: "无法解析",
			dsn:  "://///",
			want: dsnOpaque,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := redactDSN(tt.dsn)

			assert.Equal(t, tt.want, got)
			if tt.dsn != "" {
				assert.NotContains(t, got, dsnPassword, "脱敏结果仍含密码")
			}
		})
	}
}
