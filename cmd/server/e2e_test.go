package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDSN is the connection string every started server receives. It is
// package state rather than a parameter because startServer is called
// from a dozen places and the database is now required by all of them.
var testDSN string

// testJWTSecret signs tokens for the started servers. Exactly at the
// minimum length, so the same value exercises the length rule.
const testJWTSecret = "e2e-0123456789abcdef0123456789ab"

// TestEndToEnd builds the real binary, runs it as a real process, and
// drives it over a real TCP connection.
//
// Everything here is also covered by unit tests against httptest, but
// those never exercise the actual wiring: config parsing from the
// environment, migrations, the logger reaching stdout, signal handling,
// and the process exit code. This is the check that the thing you deploy
// works, not just that the packages do.
func TestEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("端到端测试需要编译并启动进程，-short 时跳过")
	}

	// The service refuses to start without a database now that it has
	// endpoints backed by one, so the whole suite needs a real instance.
	testDSN = os.Getenv("TEST_DATABASE_URL")
	if testDSN == "" {
		t.Skip("TEST_DATABASE_URL 未设置：服务已要求数据库才能启动，跳过端到端测试")
	}

	bin := buildServer(t)

	t.Run("正常启动与请求", func(t *testing.T) {
		srv := startServer(t, bin, nil)

		t.Run("启动日志是结构化 JSON", func(t *testing.T) {
			line := srv.firstLogLine(t)
			t.Logf("实际启动日志: %s", line)

			// Every line, not just the first. This is what caught gin
			// printing its debug banner to stdout: one non-JSON line
			// anywhere breaks a collector parsing line by line, and
			// checking only the first would miss it once something else
			// logs earlier.
			for _, l := range srv.logLines(t) {
				assert.True(t, json.Valid([]byte(l)),
					"日志流里有非 JSON 的行: %s", l)
			}

			rec := srv.recordWithMsg(t, "server listening")

			assert.Equal(t, "INFO", rec["level"])
			// Config is logged through slog.LogValuer as a group.
			cfg, ok := rec["config"].(map[string]any)
			require.True(t, ok, "config 应作为分组字段输出: %v", rec["config"])
			assert.Equal(t, srv.port, cfg["port"])
		})

		t.Run("health 返回 200 与 JSON", func(t *testing.T) {
			resp, body := srv.get(t, "/api/health")

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
			assert.Contains(t, body, `"status":"ok"`)
		})

		t.Run("ready 返回 200 并报告数据库依赖", func(t *testing.T) {
			resp, body := srv.get(t, "/api/ready")

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Contains(t, body, `"status":"ready"`)
			assert.Contains(t, body, `"name":"database"`,
				"数据库已是必需依赖，就绪探针必须报告它")

			// The "empty checks serialise as [] rather than null"
			// property moved to internal/handler's
			// TestReady_NoChecksRegistered: a running server always has
			// the database check now, so it cannot be observed here.
		})

		t.Run("每个响应都带 X-Request-Id", func(t *testing.T) {
			resp, _ := srv.get(t, "/api/health")

			id := resp.Header.Get("X-Request-Id")
			assert.Regexp(t, `^[0-9a-f]{32}$`, id)
		})

		t.Run("安全响应头在真实服务器上生效", func(t *testing.T) {
			resp, _ := srv.get(t, "/api/health")

			assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			assert.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"))
			assert.Equal(t, "0", resp.Header.Get("X-XSS-Protection"))
			assert.Equal(t, "strict-origin-when-cross-origin", resp.Header.Get("Referrer-Policy"))
			assert.Contains(t, resp.Header.Get("Strict-Transport-Security"), "max-age=")
			assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
		})

		t.Run("CORS 未配置时跨域被拒绝", func(t *testing.T) {
			// The default server configures no allowed origins, so a
			// cross-origin request must get no Access-Control-Allow-Origin.
			resp, _ := srv.getWithHeader(t, "/api/health", "Origin", "https://app.example.com")

			assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"),
				"未配置 CORS_ALLOWED_ORIGINS 时应 fail-closed，不返回 ACAO")
		})

		t.Run("合法的 X-Request-Id 被透传", func(t *testing.T) {
			const sent = "trace-abc-123"
			resp, _ := srv.getWithHeader(t, "/api/health", "X-Request-Id", sent)

			assert.Equal(t, sent, resp.Header.Get("X-Request-Id"))
		})

		t.Run("非法的 X-Request-Id 被替换", func(t *testing.T) {
			// A newline payload is not testable here: Go's HTTP client
			// refuses to write a header containing one, so it never
			// reaches the server. That case is covered by the unit tests,
			// which drive the handler directly. What a real caller CAN
			// send is quotes and braces, which is the payload that would
			// forge JSON fields in the log.
			bad := []struct{ name, sent string }{
				{"超长", strings.Repeat("a", 100)},
				{"含空格与分号", "bad id;spaces"},
				{"含引号花括号（伪造字段）", `a","level":"ERROR","msg":"forged`},
				{"含制表符", "abc\tdef"},
			}

			for _, tc := range bad {
				t.Run(tc.name, func(t *testing.T) {
					resp, _ := srv.getWithHeader(t, "/api/health", "X-Request-Id", tc.sent)

					got := resp.Header.Get("X-Request-Id")
					assert.NotEqual(t, tc.sent, got)
					assert.Regexp(t, `^[0-9a-f]{32}$`, got, "应替换为新生成的 ID")
				})
			}
		})

		// The forged payload above must not survive anywhere in the log
		// stream, which is the whole reason the ID is validated.
		t.Run("伪造载荷不会进入日志", func(t *testing.T) {
			const forged = `x","level":"ERROR","msg":"payment approved`
			srv.getWithHeader(t, "/api/health", "X-Request-Id", forged)

			assert.NotContains(t, srv.output(), "payment approved")
		})

		t.Run("404 返回 JSON 而非纯文本", func(t *testing.T) {
			resp, body := srv.get(t, "/api/nope")

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
			assert.Contains(t, body, `"code":"NOT_FOUND"`)
		})

		t.Run("405 带 Allow 头", func(t *testing.T) {
			resp, body := srv.do(t, http.MethodDelete, "/api/health", nil)

			assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
			assert.Equal(t, "GET", resp.Header.Get("Allow"))
			assert.Contains(t, body, `"code":"METHOD_NOT_ALLOWED"`)
		})

		t.Run("访问日志字段齐全且与响应头一致", func(t *testing.T) {
			resp, _ := srv.get(t, "/api/info")
			wantID := resp.Header.Get("X-Request-Id")

			// The record is written after the response is sent.
			var rec map[string]any
			require.Eventually(t, func() bool {
				for _, r := range srv.records(t) {
					if r["msg"] == "request" && r["request_id"] == wantID {
						rec = r
						return true
					}
				}
				return false
			}, 2*time.Second, 20*time.Millisecond, "没有找到对应的访问日志")

			assert.Equal(t, "GET", rec["method"])
			assert.Equal(t, "/api/info", rec["path"])
			assert.EqualValues(t, http.StatusOK, rec["status"])
			assert.NotNil(t, rec["duration_ms"])
			assert.NotEmpty(t, rec["client_ip"])
		})

		t.Run("SIGTERM 优雅关闭", func(t *testing.T) {
			code := srv.terminate(t)

			assert.Equal(t, 0, code, "优雅关闭应以 0 退出")
			assert.Contains(t, srv.output(), "server stopped cleanly")
		})
	})

	t.Run("LOG_FORMAT=text 切换输出格式", func(t *testing.T) {
		srv := startServer(t, bin, map[string]string{"LOG_FORMAT": "text"})
		defer srv.terminate(t)

		var listening string
		require.Eventually(t, func() bool {
			for _, line := range srv.logLines(t) {
				if strings.Contains(line, `msg="server listening"`) {
					listening = line
					return true
				}
			}
			return false
		}, 5*time.Second, 20*time.Millisecond, "没有找到启动日志:\n%s", srv.output())

		assert.False(t, json.Valid([]byte(listening)),
			"text 格式不应是 JSON: %s", listening)
		assert.Contains(t, listening, "level=INFO")
	})

	// The point of validating config at startup: a typo stops the
	// process with an explanation instead of silently using a default.
	t.Run("非法配置导致启动失败", func(t *testing.T) {
		tests := []struct {
			name    string
			env     map[string]string
			wantMsg string
		}{
			{
				name:    "端口不是数字",
				env:     map[string]string{"PORT": "abc"},
				wantMsg: `PORT must be a number`,
			},
			{
				name:    "日志格式不支持",
				env:     map[string]string{"LOG_FORMAT": "xml"},
				wantMsg: `LOG_FORMAT must be`,
			},
			{
				name: "请求超时不短于写超时",
				env: map[string]string{
					"REQUEST_TIMEOUT": "30s",
					"WRITE_TIMEOUT":   "10s",
				},
				wantMsg: "must be shorter than WRITE_TIMEOUT",
			},
			{
				// Became required once endpoints started reading and
				// writing the database; it was optional before that.
				name:    "缺少数据库连接串",
				env:     map[string]string{"DATABASE_URL": ""},
				wantMsg: "DATABASE_URL is required",
			},
			{
				name:    "缺少 JWT 密钥",
				env:     map[string]string{"JWT_SECRET": ""},
				wantMsg: "JWT_SECRET is required",
			},
			{
				// A short HMAC key can be recovered offline from any
				// captured token, after which anyone can mint tokens.
				name:    "JWT 密钥过短",
				env:     map[string]string{"JWT_SECRET": "too-short"},
				wantMsg: "JWT_SECRET must be at least 32 bytes",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cmd := exec.Command(bin)
				cmd.Env = envWith(tt.env)

				out, err := cmd.CombinedOutput()

				require.Error(t, err, "非法配置必须让进程退出，输出: %s", out)
				assert.Contains(t, string(out), tt.wantMsg)
			})
		}
	})

	t.Run("端口被占用时退出码非零", func(t *testing.T) {
		srv := startServer(t, bin, nil)
		defer srv.terminate(t)

		cmd := exec.Command(bin)
		cmd.Env = envWith(map[string]string{"PORT": srv.port})

		out, err := cmd.CombinedOutput()

		require.Error(t, err, "端口冲突必须以非零码退出，输出: %s", out)
		assert.Contains(t, string(out), "address already in use")
	})

	t.Run("带数据库", func(t *testing.T) {
		dsn := testDSN
		srv := startServer(t, bin, nil)

		t.Run("ready 报告 database 检查为 ok", func(t *testing.T) {
			resp, body := srv.get(t, "/api/ready")

			require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)

			var got struct {
				Status string `json:"status"`
				Checks []struct {
					Name       string `json:"name"`
					Status     string `json:"status"`
					DurationMS int64  `json:"duration_ms"`
					Error      string `json:"error"`
				} `json:"checks"`
			}
			require.NoError(t, json.Unmarshal([]byte(body), &got))

			assert.Equal(t, "ready", got.Status)
			require.Len(t, got.Checks, 1, "应只有 database 一个检查")
			assert.Equal(t, "database", got.Checks[0].Name)
			assert.Equal(t, "ok", got.Checks[0].Status)
			assert.Empty(t, got.Checks[0].Error)
		})

		t.Run("health 不受数据库影响", func(t *testing.T) {
			resp, _ := srv.get(t, "/api/health")
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		// The startup record dumps the whole config, so this is where a
		// DSN password would end up on disk if redaction regressed.
		t.Run("启动日志不含数据库密码", func(t *testing.T) {
			password := passwordOf(t, dsn)
			if password == "" {
				t.Skip("TEST_DATABASE_URL 里没有密码，无从验证")
			}

			assert.NotContains(t, srv.output(), password,
				"启动日志泄露了数据库密码")

			// Assert on the field rather than on a line position. An
			// earlier version checked the first log line and broke as
			// soon as another record was emitted before this one.
			rec := srv.recordWithMsg(t, "server listening")
			cfg, ok := rec["config"].(map[string]any)
			require.True(t, ok, "config 应作为分组字段输出: %v", rec["config"])

			logged, _ := cfg["database_url"].(string)
			assert.Contains(t, logged, "xxxxx", "口令位置应是脱敏后的占位符")
			assert.NotContains(t, logged, password)
			// Host and database survive redaction, which is what makes
			// the record useful for confirming where it connected.
			assert.Contains(t, logged, "go_http_service")
		})

		t.Run("启动日志不含 JWT 密钥", func(t *testing.T) {
			assert.NotContains(t, srv.output(), testJWTSecret,
				"启动日志泄露了 JWT 密钥")

			rec := srv.recordWithMsg(t, "server listening")
			cfg, ok := rec["config"].(map[string]any)
			require.True(t, ok, "config 应作为分组字段输出: %v", rec["config"])
			assert.Equal(t, "(set)", cfg["jwt_secret"], "只应报告是否已配置")
		})

		// This is the assertion the whole step exists for. The ordering
		// is produced by defer being LIFO while srv.Shutdown is an
		// ordinary call, and it is the one part of the shutdown design
		// that no unit test can demonstrate.
		t.Run("关闭顺序：先排空请求再关连接池", func(t *testing.T) {
			code := srv.terminate(t)
			require.Equal(t, 0, code)

			out := srv.output()
			drained := strings.Index(out, "server stopped cleanly")
			poolClosed := strings.Index(out, "closing database pool")

			require.NotEqual(t, -1, drained, "没有找到排空完成的日志:\n%s", out)
			require.NotEqual(t, -1, poolClosed, "没有找到关闭连接池的日志:\n%s", out)

			assert.Less(t, drained, poolClosed,
				"连接池在 HTTP 请求排空之前就关了，会切断仍在执行的查询")
		})
	})

	// The first vertical slice: HTTP through service and repository to
	// SQL and back. Unit tests cover each layer with the next one
	// stubbed; only this proves they fit together.
	t.Run("注册接口", func(t *testing.T) {
		srv := startServer(t, bin, nil)

		username := fmt.Sprintf("e2e%d", time.Now().UnixNano())
		cleanupUsers(t, username)

		email := username + "@example.com"
		body := fmt.Sprintf(
			`{"username":%q,"email":%q,"password":"correct-horse-battery"}`, username, email)

		var created struct {
			ID        int64     `json:"id"`
			Username  string    `json:"username"`
			Email     string    `json:"email"`
			CreatedAt time.Time `json:"created_at"`
		}

		t.Run("注册成功返回 201", func(t *testing.T) {
			resp, raw := srv.post(t, "/api/users", body)

			require.Equal(t, http.StatusCreated, resp.StatusCode, "body: %s", raw)
			require.NoError(t, json.Unmarshal([]byte(raw), &created))

			assert.Positive(t, created.ID, "id 应由数据库生成")
			assert.Equal(t, username, created.Username)
			assert.Equal(t, email, created.Email)
			assert.False(t, created.CreatedAt.IsZero())
		})

		t.Run("响应不含任何密码材料", func(t *testing.T) {
			_, raw := srv.post(t, "/api/users",
				fmt.Sprintf(`{"username":"%s2","email":"2%s","password":"correct-horse-battery"}`,
					username, email))

			for _, leak := range []string{"password", "$2a$", "correct-horse-battery"} {
				assert.NotContains(t, raw, leak, "响应泄露了 %q: %s", leak, raw)
			}
		})

		t.Run("重复用户名返回 409", func(t *testing.T) {
			resp, raw := srv.post(t, "/api/users", body)

			require.Equal(t, http.StatusConflict, resp.StatusCode, "body: %s", raw)
			assert.Contains(t, raw, `"code":"CONFLICT"`)
		})

		// Proves the functional unique index on lower(username) reaches
		// all the way out to the HTTP contract.
		t.Run("大小写不同的同名也返回 409", func(t *testing.T) {
			upper := fmt.Sprintf(
				`{"username":%q,"email":"upper-%s","password":"correct-horse-battery"}`,
				strings.ToUpper(username), email)

			resp, raw := srv.post(t, "/api/users", upper)

			require.Equal(t, http.StatusConflict, resp.StatusCode, "body: %s", raw)
		})

		t.Run("校验失败返回 400 且不泄露内部细节", func(t *testing.T) {
			resp, raw := srv.post(t, "/api/users", `{"username":"ab","email":"nope","password":"x"}`)

			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			assert.Contains(t, raw, `"code":"VALIDATION_FAILED"`)
			for _, leak := range []string{"CreateUserRequest", "struct", "Key:"} {
				assert.NotContains(t, raw, leak)
			}
		})

		t.Run("迁移已在启动时执行", func(t *testing.T) {
			rec := srv.recordWithMsg(t, "server listening")
			require.NotNil(t, rec)
			// A successful registration above already proves the users
			// table exists; this just confirms the server got that far.
			assert.Equal(t, "INFO", rec["level"])
		})
	})

	// The full credential lifecycle against a real process: an account
	// is created, exchanged for a token, and the token opens a door that
	// is otherwise shut.
	t.Run("登录与受保护接口", func(t *testing.T) {
		// Auth exercises login/refresh/logout many times from one address.
		// The dedicated rate-limit case already covers the tight budget;
		// this one needs enough headroom to finish the credential lifecycle.
		srv := startServer(t, bin, map[string]string{
			"LOGIN_RATE_LIMIT_RPM":   "60",
			"LOGIN_RATE_LIMIT_BURST": "30",
		})

		username := fmt.Sprintf("auth%d", time.Now().UnixNano())
		cleanupUsers(t, username)

		const password = "correct-horse-battery"
		register := fmt.Sprintf(
			`{"username":%q,"email":"%s@example.com","password":%q}`,
			username, username, password)

		resp, raw := srv.post(t, "/api/users", register)
		require.Equal(t, http.StatusCreated, resp.StatusCode, "body: %s", raw)

		credentials := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)

		var (
			token      string
			refresh    string
			oldRefresh string
		)

		t.Run("登录返回 access 与 refresh", func(t *testing.T) {
			resp, raw := srv.post(t, "/api/auth/login", credentials)
			require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", raw)

			var pair struct {
				AccessToken  string    `json:"access_token"`
				RefreshToken string    `json:"refresh_token"`
				TokenType    string    `json:"token_type"`
				ExpiresAt    time.Time `json:"expires_at"`
			}
			require.NoError(t, json.Unmarshal([]byte(raw), &pair))

			assert.NotEmpty(t, pair.AccessToken)
			assert.NotEmpty(t, pair.RefreshToken)
			assert.Equal(t, "Bearer", pair.TokenType)
			assert.True(t, pair.ExpiresAt.After(time.Now()), "token 应尚未过期")

			// A JWT has three dot-separated parts.
			assert.Len(t, strings.Split(pair.AccessToken, "."), 3)

			token = pair.AccessToken
			refresh = pair.RefreshToken
		})

		t.Run("refresh token 入库的是哈希", func(t *testing.T) {
			require.NotEmpty(t, refresh, "依赖前一个子测试拿到的 refresh token")

			hash := activeRefreshHash(t, username)
			assert.Len(t, hash, 64, "SHA-256 十六进制固定 64 字符")
			assert.NotEqual(t, refresh, hash, "库里绝不能是 token 原文")
			assert.NotContains(t, hash, "$2", "refresh token 不该用 bcrypt")
		})

		t.Run("登录响应不含用户信息", func(t *testing.T) {
			_, raw := srv.post(t, "/api/auth/login", credentials)

			for _, leak := range []string{"password", "$2a$", "@example.com", "token_hash"} {
				assert.NotContains(t, raw, leak, "登录响应泄露了 %q", leak)
			}
		})

		t.Run("带 token 可以访问 me", func(t *testing.T) {
			require.NotEmpty(t, token, "依赖前一个子测试拿到的 token")

			resp, raw := srv.getWithHeader(t, "/api/auth/me", "Authorization", "Bearer "+token)

			require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", raw)
			assert.Contains(t, raw, username)
			assert.NotContains(t, raw, "password")
		})

		t.Run("不带 token 访问 me 返回 401", func(t *testing.T) {
			resp, raw := srv.get(t, "/api/auth/me")

			require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			assert.Contains(t, raw, `"code":"UNAUTHORIZED"`)
			assert.Equal(t, `Bearer realm="api"`, resp.Header.Get("WWW-Authenticate"))
		})

		// Every rejection must look the same, so a caller learns nothing
		// about how close their credential was to working.
		t.Run("各种无效 token 的响应完全一致", func(t *testing.T) {
			var bodies []string

			for _, header := range []string{
				"",
				"Bearer",
				"Bearer not-a-jwt",
				"Basic " + token,
				"Bearer " + token + "tampered",
			} {
				resp, raw := srv.getWithHeader(t, "/api/auth/me", "Authorization", header)
				require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
					"header %q 应被拒绝，实际 %d", header, resp.StatusCode)
				bodies = append(bodies, raw)
			}

			for i := 1; i < len(bodies); i++ {
				assert.Equal(t, bodies[0], bodies[i],
					"第 %d 种无效 token 的响应与其他不同", i)
			}
		})

		t.Run("密码错误与用户不存在响应一致", func(t *testing.T) {
			_, wrongPassword := srv.post(t, "/api/auth/login",
				fmt.Sprintf(`{"username":%q,"password":"definitely-wrong"}`, username))

			_, noSuchUser := srv.post(t, "/api/auth/login",
				`{"username":"nobody-at-all-12345","password":"definitely-wrong"}`)

			assert.Equal(t, wrongPassword, noSuchUser,
				"两者响应必须一模一样，否则接口就是用户名枚举器")
		})

		t.Run("刷新返回新的一对", func(t *testing.T) {
			require.NotEmpty(t, refresh, "依赖登录拿到的 refresh token")

			resp, raw := srv.post(t, "/api/auth/refresh",
				fmt.Sprintf(`{"refresh_token":%q}`, refresh))
			require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", raw)

			var pair struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			}
			require.NoError(t, json.Unmarshal([]byte(raw), &pair))

			assert.NotEmpty(t, pair.AccessToken)
			assert.NotEmpty(t, pair.RefreshToken)
			assert.NotEqual(t, refresh, pair.RefreshToken, "轮转必须换发新的 refresh token")
			assert.Len(t, strings.Split(pair.AccessToken, "."), 3)

			oldRefresh = refresh
			token = pair.AccessToken
			refresh = pair.RefreshToken
		})

		t.Run("重放已用过的 refresh 返回 401", func(t *testing.T) {
			require.NotEmpty(t, oldRefresh, "依赖刷新子测试留下的旧 token")

			resp, replay := srv.post(t, "/api/auth/refresh",
				fmt.Sprintf(`{"refresh_token":%q}`, oldRefresh))
			require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			assert.Contains(t, replay, `"code":"UNAUTHORIZED"`)

			_, unknown := srv.post(t, "/api/auth/refresh",
				`{"refresh_token":"not-a-real-token"}`)
			assert.Equal(t, unknown, replay,
				"重放与无效 token 的响应必须一模一样")
		})

		t.Run("登出后无法再刷新", func(t *testing.T) {
			// Replay above family-revoked every session, so this path
			// starts from a fresh login.
			resp, raw := srv.post(t, "/api/auth/login", credentials)
			require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", raw)

			var pair struct {
				RefreshToken string `json:"refresh_token"`
			}
			require.NoError(t, json.Unmarshal([]byte(raw), &pair))
			require.NotEmpty(t, pair.RefreshToken)

			resp, raw = srv.post(t, "/api/auth/logout",
				fmt.Sprintf(`{"refresh_token":%q}`, pair.RefreshToken))
			require.Equal(t, http.StatusNoContent, resp.StatusCode, "body: %s", raw)
			assert.Empty(t, raw)

			resp, raw = srv.post(t, "/api/auth/refresh",
				fmt.Sprintf(`{"refresh_token":%q}`, pair.RefreshToken))
			require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			assert.Contains(t, raw, `"code":"UNAUTHORIZED"`)
		})
	})

	// Login carries its own, far tighter budget than the rest of the API.
	t.Run("登录接口限流更严", func(t *testing.T) {
		srv := startServer(t, bin, map[string]string{
			"LOGIN_RATE_LIMIT_RPM":   "60",
			"LOGIN_RATE_LIMIT_BURST": "3",
		})

		body := `{"username":"whoever","password":"whatever"}`

		var statuses []int
		for range 6 {
			resp, _ := srv.post(t, "/api/auth/login", body)
			statuses = append(statuses, resp.StatusCode)
		}

		assert.Contains(t, statuses, http.StatusTooManyRequests,
			"连打登录接口应触发限流，实际状态码: %v", statuses)

		// The probes stay reachable even while login is being throttled.
		resp, _ := srv.get(t, "/api/health")
		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"登录被限流时探针仍须可用")
	})

	t.Run("数据库连不上时拒绝启动", func(t *testing.T) {
		cmd := exec.Command(bin)
		// 192.0.2.1 是 RFC 5737 保留的不可路由地址。
		cmd.Env = envWith(map[string]string{
			"DATABASE_URL":       "postgres://app:pw@192.0.2.1:5432/db",
			"DB_CONNECT_TIMEOUT": "500ms",
		})

		out, err := cmd.CombinedOutput()

		require.Error(t, err, "连不上数据库必须以非零码退出，输出: %s", out)
		assert.Contains(t, string(out), "database")
		assert.NotContains(t, string(out), "pw@", "错误信息不该带连接串")
	})
}

// passwordOf extracts the password from a URL-form DSN, or "" if there
// is none to check.
func passwordOf(t *testing.T, dsn string) string {
	t.Helper()

	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return ""
	}
	password, _ := u.User.Password()
	return password
}

// buildServer compiles the package under test into a temporary binary.
func buildServer(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "server")
	cmd := exec.Command("go", "build", "-o", bin, ".")

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "编译失败: %s", out)

	return bin
}

// server is a running instance under test.
type server struct {
	port string
	cmd  *exec.Cmd

	mu   sync.Mutex
	logs strings.Builder

	// logsDone closes when the stdout scanner reaches EOF, which happens
	// once the process exits and closes its end of the pipe.
	logsDone chan struct{}

	stopped bool
}

// startServer launches the binary on a free port and waits until it
// answers requests.
func startServer(t *testing.T, bin string, env map[string]string) *server {
	t.Helper()

	port := freePort(t)

	full := map[string]string{
		"PORT": port,
		// Both required for the process to start at all. A caller can
		// still override either, which is how the failure cases work.
		"DATABASE_URL": testDSN,
		"JWT_SECRET":   testJWTSecret,
	}
	for k, v := range env {
		full[k] = v
	}

	cmd := exec.Command(bin)
	cmd.Env = envWith(full)

	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = cmd.Stdout

	require.NoError(t, cmd.Start())

	s := &server{port: port, cmd: cmd, logsDone: make(chan struct{})}

	// Drain the pipe continuously; a full pipe would block the process.
	go func() {
		defer close(s.logsDone)

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			s.mu.Lock()
			s.logs.WriteString(scanner.Text())
			s.logs.WriteString("\n")
			s.mu.Unlock()
		}
	}()

	t.Cleanup(func() { s.terminate(t) })

	s.waitUntilServing(t)
	return s
}

// waitUntilServing polls until the health endpoint answers, or fails
// fast if the process gave up first.
func (s *server) waitUntilServing(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// A process that exited closes its end of the pipe, which ends
		// the scanner and closes logsDone. Checking that turns a
		// configuration error from a ten-second wait into an immediate
		// failure carrying the reason - six such cases used to cost a
		// minute between them.
		select {
		case <-s.logsDone:
			t.Fatalf("服务启动失败并已退出，日志:\n%s", s.output())
		default:
		}

		resp, err := http.Get(s.url("/api/health"))
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("服务在 10s 内没有就绪，日志:\n%s", s.output())
}

func (s *server) url(path string) string {
	return "http://127.0.0.1:" + s.port + path
}

func (s *server) get(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	return s.do(t, http.MethodGet, path, nil)
}

func (s *server) getWithHeader(t *testing.T, path, key, value string) (*http.Response, string) {
	t.Helper()
	return s.do(t, http.MethodGet, path, map[string]string{key: value})
}

func (s *server) do(t *testing.T, method, path string, headers map[string]string) (*http.Response, string) {
	t.Helper()

	req, err := http.NewRequest(method, s.url(path), nil)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp, string(body)
}

func (s *server) output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logs.String()
}

// post sends a JSON body and returns the response with its text.
func (s *server) post(t *testing.T, path, body string) (*http.Response, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, s.url(path), strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp, string(raw)
}

// activeRefreshHash returns the newest unrevoked refresh-token hash for
// username. Used to prove the raw token never lands in the database.
func activeRefreshHash(t *testing.T, username string) string {
	t.Helper()

	ctx := t.Context()
	pool, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pool.Close()

	var hash string
	err = pool.QueryRow(ctx, `
		SELECT token_hash FROM refresh_tokens
		WHERE user_id = (SELECT id FROM users WHERE username = $1)
		  AND revoked_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`, username).Scan(&hash)
	require.NoError(t, err)
	return hash
}

// cleanupUsers removes the accounts a test created. The development
// database is long-lived, so without this every run leaves rows behind.
func cleanupUsers(t *testing.T, prefix string) {
	t.Helper()

	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())

		pool, err := pgxpool.New(ctx, testDSN)
		if err != nil {
			t.Logf("清理测试用户失败（连接）: %v", err)
			return
		}
		defer pool.Close()

		if _, err := pool.Exec(ctx,
			`DELETE FROM users WHERE username LIKE $1`, prefix+"%"); err != nil {
			t.Logf("清理测试用户失败（删除）: %v", err)
		}
	})
}

// firstLogLine returns the startup record, waiting for it to appear.
func (s *server) firstLogLine(t *testing.T) string {
	t.Helper()

	var line string
	require.Eventually(t, func() bool {
		out := strings.TrimSpace(s.output())
		if out == "" {
			return false
		}
		line = strings.SplitN(out, "\n", 2)[0]
		return true
	}, 5*time.Second, 20*time.Millisecond, "没有收到任何日志")

	return line
}

// records parses the JSON log lines emitted so far.
func (s *server) records(t *testing.T) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, line := range s.logLines(t) {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // text-format runs, or a partial line
		}
		out = append(out, rec)
	}
	return out
}

// logLines returns the non-empty lines emitted so far.
func (s *server) logLines(t *testing.T) []string {
	t.Helper()

	var out []string
	for _, line := range strings.Split(s.output(), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// recordWithMsg waits for and returns the record carrying the given msg.
// Looking a record up by name rather than by position keeps the
// assertions stable when something else starts logging earlier.
func (s *server) recordWithMsg(t *testing.T, msg string) map[string]any {
	t.Helper()

	var found map[string]any
	require.Eventually(t, func() bool {
		for _, rec := range s.records(t) {
			if rec["msg"] == msg {
				found = rec
				return true
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond, "没有找到 msg=%q 的日志:\n%s", msg, s.output())

	return found
}

// terminate sends SIGTERM and returns the exit code. Safe to call twice,
// since both the test body and t.Cleanup invoke it.
func (s *server) terminate(t *testing.T) int {
	t.Helper()

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return 0
	}
	s.stopped = true
	s.mu.Unlock()

	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return -1
	}

	// Wait for the scanner to hit EOF BEFORE calling cmd.Wait.
	//
	// os/exec closes the pipe returned by StdoutPipe as soon as Wait
	// sees the process exit, so calling Wait first races the reader and
	// silently truncates whatever the process logged on its way out.
	// That is exactly the shutdown sequence this suite asserts on, and
	// it passed for a while only because there was less to write.
	select {
	case <-s.logsDone:
	case <-time.After(20 * time.Second):
		_ = s.cmd.Process.Kill()
		t.Fatal("日志管道在 20s 内没有关闭")
		return -1
	}

	if err := s.cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if asExitError(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return -1
	}
	return 0
}

func asExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

// freePort asks the kernel for an unused port and releases it. There is
// a small race before the server binds it, which is acceptable here.
func freePort(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()

	return fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
}

// envWith returns the current environment with the service variables
// cleared and the given overrides applied, so a value inherited from the
// developer's shell cannot influence a case.
func envWith(overrides map[string]string) []string {
	managed := []string{
		"PORT", "TRUSTED_PROXIES", "CORS_ALLOWED_ORIGINS",
		"READ_HEADER_TIMEOUT", "READ_TIMEOUT", "WRITE_TIMEOUT", "IDLE_TIMEOUT",
		"SHUTDOWN_TIMEOUT", "REQUEST_TIMEOUT",
		"MAX_BODY_BYTES", "LOG_LEVEL", "LOG_FORMAT",
		"DATABASE_URL", "DB_MAX_CONNS", "DB_CONNECT_TIMEOUT",
		"RATE_LIMIT_RPS", "RATE_LIMIT_BURST",
		"LOGIN_RATE_LIMIT_RPM", "LOGIN_RATE_LIMIT_BURST",
		"JWT_SECRET", "ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL",
	}

	drop := make(map[string]bool, len(managed))
	for _, k := range managed {
		drop[k] = true
	}

	env := make([]string, 0, len(os.Environ())+len(overrides)+1)
	for _, kv := range os.Environ() {
		if key, _, ok := strings.Cut(kv, "="); ok && drop[key] {
			continue
		}
		env = append(env, kv)
	}

	// Seeded before the overrides so a case can blank either to exercise
	// the missing-configuration paths.
	env = append(env, "DATABASE_URL="+testDSN, "JWT_SECRET="+testJWTSecret)

	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	return env
}
