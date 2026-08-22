package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-http-service/internal/config"
)

// testPassword appears in every DSN these tests build, so any assertion
// that it did not leak is checking something real.
const testPassword = "sup3rs3cr3t"

// testConfig returns settings with a short timeout, so the failure paths
// do not spend seconds waiting on an unreachable host.
func testConfig(dsn string) *config.Config {
	return &config.Config{
		DatabaseURL:      dsn,
		DBMaxConns:       config.DefaultDBMaxConns,
		DBConnectTimeout: 500 * time.Millisecond,
	}
}

// requireTestDatabase returns the DSN of a real database, or skips.
//
// Database-backed tests are opt-in so `go test ./...` stays green on a
// machine with no PostgreSQL. CI sets this variable, so the path is
// still exercised on every push.
func requireTestDatabase(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL 未设置，跳过需要真实数据库的测试")
	}
	return dsn
}

func TestConnect_InvalidDSN(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, dsn string }{
		{"完全不是连接串", "://///"},
		{"scheme 不支持", "mysql://app:" + testPassword + "@localhost:3306/db"},
		{"端口不是数字", "postgres://app:" + testPassword + "@localhost:abc/db"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool, err := Connect(t.Context(), testConfig(tt.dsn))

			require.Error(t, err)
			assert.Nil(t, pool)
			assert.True(t, errors.Is(err, ErrInvalidDSN),
				"应返回 ErrInvalidDSN，实际: %v", err)
		})
	}
}

// TestConnect_ErrorNeverLeaksPassword is the reason ErrInvalidDSN drops
// the underlying parse error instead of wrapping it. pgx reports the
// connection string it failed on, and main logs whatever Connect
// returns, so wrapping would write the credential to disk on every bad
// start.
func TestConnect_ErrorNeverLeaksPassword(t *testing.T) {
	t.Parallel()

	dsns := []string{
		"postgres://app:" + testPassword + "@localhost:abc/db",
		"mysql://app:" + testPassword + "@localhost:3306/db",
		// Unreachable rather than unparseable: a different error path.
		"postgres://app:" + testPassword + "@192.0.2.1:5432/db",
	}

	for _, dsn := range dsns {
		_, err := Connect(t.Context(), testConfig(dsn))

		require.Error(t, err)
		assert.NotContains(t, err.Error(), testPassword,
			"错误信息泄露了密码: %v", err)
	}
}

// TestConnect_UnreachableHost covers the point of pinging during
// Connect. pgxpool.NewWithConfig is lazy, so without the explicit ping
// this would succeed and the failure would surface on the first request
// instead of at startup.
//
// 192.0.2.1 is TEST-NET-1, reserved by RFC 5737 and not routable.
func TestConnect_UnreachableHost(t *testing.T) {
	t.Parallel()

	cfg := testConfig("postgres://app:" + testPassword + "@192.0.2.1:5432/db")

	start := time.Now()
	pool, err := Connect(t.Context(), cfg)
	elapsed := time.Since(start)

	require.Error(t, err, "连不上的主机必须在 Connect 阶段就报错")
	assert.Nil(t, pool)
	assert.Less(t, elapsed, 5*time.Second,
		"应受 DBConnectTimeout 约束，实际耗时 %s", elapsed)
}

func TestConnect_RespectsCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	pool, err := Connect(ctx, testConfig("postgres://app:pw@192.0.2.1:5432/db"))

	require.Error(t, err)
	assert.Nil(t, pool)
}

func TestConnect_Succeeds(t *testing.T) {
	dsn := requireTestDatabase(t)

	pool, err := Connect(t.Context(), testConfig(dsn))
	require.NoError(t, err)
	require.NotNil(t, pool)
	defer pool.Close()

	assert.NoError(t, pool.Ping(t.Context()))
	assert.EqualValues(t, config.DefaultDBMaxConns, pool.Config().MaxConns,
		"MaxConns 应来自配置而非 pgx 的默认值（后者随机器 CPU 数变化）")
}

func TestHealthCheck(t *testing.T) {
	dsn := requireTestDatabase(t)

	pool, err := Connect(t.Context(), testConfig(dsn))
	require.NoError(t, err)

	check := HealthCheck(pool, time.Second)

	assert.NoError(t, check(t.Context()), "库正常时探针应通过")

	// Once the pool is closed the probe must report failure, which is
	// what turns /api/ready into a 503.
	pool.Close()
	assert.Error(t, check(t.Context()), "池关闭后探针必须失败")
}

// TestHealthCheck_HonoursCallerDeadline confirms the probe cannot outlive
// the request that triggered it, even when its own timeout is generous.
func TestHealthCheck_HonoursCallerDeadline(t *testing.T) {
	dsn := requireTestDatabase(t)

	pool, err := Connect(t.Context(), testConfig(dsn))
	require.NoError(t, err)
	defer pool.Close()

	// A minute of probe timeout against an already-expired request.
	check := HealthCheck(pool, time.Minute)

	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)

	assert.Error(t, check(ctx), "调用方的 deadline 应优先于探针自身的超时")
}
