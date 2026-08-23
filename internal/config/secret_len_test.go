package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go-http-service/internal/auth"
	"go-http-service/internal/config"
)

// TestMinJWTSecretLenMatchesAuth guards a deliberate duplication.
//
// config must not import other internal packages - it is the leaf every
// one of them depends on - so the minimum secret length is written down
// twice. That is fine as long as the two cannot drift apart silently,
// which is what this test is for. It lives in an external test package
// so importing auth here does not create the dependency config avoids.
func TestMinJWTSecretLenMatchesAuth(t *testing.T) {
	t.Parallel()

	assert.Equal(t, auth.MinSecretLen, config.MinJWTSecretLen,
		"config.MinJWTSecretLen 与 auth.MinSecretLen 必须一致，"+
			"否则 config 放行的密钥会被 auth 拒绝，服务在启动最后一刻才失败")
}
