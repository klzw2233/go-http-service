package db

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// migrateTestPool connects to the test database and returns a pool.
func migrateTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := requireTestDatabase(t)

	pool, err := Connect(t.Context(), testConfig(dsn))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func TestMigrate_CreatesSchema(t *testing.T) {
	pool := migrateTestPool(t)

	require.NoError(t, Migrate(t.Context(), pool, discardLogger()))

	var exists bool
	err := pool.QueryRow(t.Context(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'users')`).
		Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "users 表应已创建")

	// The bookkeeping table records what ran.
	var version string
	err = pool.QueryRow(t.Context(),
		`SELECT version FROM schema_migrations ORDER BY version LIMIT 1`).Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, "0001_create_users", version)
}

// TestMigrate_IsIdempotent is the property that makes it safe to run on
// every start: a second pass must apply nothing.
func TestMigrate_IsIdempotent(t *testing.T) {
	pool := migrateTestPool(t)
	ctx := t.Context()

	require.NoError(t, Migrate(ctx, pool, discardLogger()))

	var before int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_migrations`).Scan(&before))

	require.NoError(t, Migrate(ctx, pool, discardLogger()), "重复执行不应报错")

	var after int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_migrations`).Scan(&after))

	assert.Equal(t, before, after, "第二次执行不应新增任何记录")
}

// TestMigrate_ConcurrentRunsAreSerialised covers the advisory lock. Two
// replicas starting together is the normal case under an orchestrator,
// and without the lock they would race to create the same table - one
// would fail with "relation already exists".
func TestMigrate_ConcurrentRunsAreSerialised(t *testing.T) {
	pool := migrateTestPool(t)
	ctx := t.Context()

	const racers = 4
	errs := make([]error, racers)

	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // 尽可能同时开跑
			errs[i] = Migrate(ctx, pool, discardLogger())
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "第 %d 个并发迁移失败了", i)
	}

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_migrations WHERE version = '0001_create_users'`).
		Scan(&count))
	assert.Equal(t, 1, count, "并发执行不应产生重复记录")
}

// TestMigrate_ReleasesLock proves the lock is not left held, which would
// deadlock the next deployment.
func TestMigrate_ReleasesLock(t *testing.T) {
	pool := migrateTestPool(t)
	ctx := t.Context()

	require.NoError(t, Migrate(ctx, pool, discardLogger()))

	// The check has to come from a different session. Advisory locks are
	// re-entrant within the session that holds them, so asking the same
	// pool would succeed even if the lock had leaked.
	other := migrateTestPool(t)

	conn, err := other.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	var acquired bool
	require.NoError(t, conn.QueryRow(ctx,
		"SELECT pg_try_advisory_lock($1)", migrationLockID).Scan(&acquired))

	assert.True(t, acquired, "迁移结束后 advisory lock 必须已释放，否则下次部署会卡死")

	if acquired {
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID)
	}
}

func TestPendingMigrations_SkipsApplied(t *testing.T) {
	t.Parallel()

	all, err := pendingMigrations(map[string]bool{})
	require.NoError(t, err)
	require.NotEmpty(t, all, "应至少有一个迁移文件")

	// Files are applied in filename order, which is why they carry a
	// zero-padded numeric prefix.
	for i := 1; i < len(all); i++ {
		assert.Less(t, all[i-1].version, all[i].version, "迁移必须按文件名有序")
	}

	none, err := pendingMigrations(map[string]bool{all[0].version: true})
	require.NoError(t, err)
	assert.Len(t, none, len(all)-1, "已应用的迁移不应再次返回")
}

func TestMigrate_RespectsCancelledContext(t *testing.T) {
	pool := migrateTestPool(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.Error(t, Migrate(ctx, pool, discardLogger()))
}
