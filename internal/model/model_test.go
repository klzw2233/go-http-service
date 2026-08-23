package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testTime = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

func TestNewHealthResponse(t *testing.T) {
	t.Parallel()

	got := NewHealthResponse(testTime)

	assert.Equal(t, StatusOK, got.Status)
	assert.True(t, got.Timestamp.Equal(testTime))
}

func TestNewInfoResponse(t *testing.T) {
	t.Parallel()

	got := NewInfoResponse(testTime)

	assert.Equal(t, Name, got.Name)
	assert.Equal(t, Version, got.Version)
	assert.Equal(t, runtime.Version(), got.GoVersion)
	assert.True(t, got.Timestamp.Equal(testTime))
}

func TestNewEchoResponse(t *testing.T) {
	t.Parallel()

	got := NewEchoResponse("hello", testTime)

	assert.Equal(t, "hello", got.Message)
	assert.True(t, got.EchoedAt.Equal(testTime))
}

// TestEchoRequestMaxTagMatchesConstant guards the one duplication the
// language forces on us: struct tags cannot reference constants, so
// EchoRequest repeats MaxEchoMessageRunes as a literal. If the two ever
// drift, the documented limit and the enforced limit disagree silently.
func TestEchoRequestMaxTagMatchesConstant(t *testing.T) {
	t.Parallel()

	field, ok := reflect.TypeOf(EchoRequest{}).FieldByName("Message")
	require.True(t, ok, "EchoRequest.Message not found")

	tag := field.Tag.Get("binding")
	require.NotEmpty(t, tag, "Message has no binding tag")

	var tagMax string
	for _, rule := range strings.Split(tag, ",") {
		if after, found := strings.CutPrefix(rule, "max="); found {
			tagMax = after
			break
		}
	}
	require.NotEmpty(t, tagMax, "binding tag %q has no max= rule", tag)

	parsed, err := strconv.Atoi(tagMax)
	require.NoError(t, err)

	assert.Equal(t, MaxEchoMessageRunes, parsed,
		"binding tag says max=%d but MaxEchoMessageRunes is %d", parsed, MaxEchoMessageRunes)
}

// TestJSONFieldNames pins the wire format. These names are the public
// contract; renaming a Go field must not silently rename them.
func TestJSONFieldNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  []string
	}{
		{"HealthResponse", NewHealthResponse(testTime), []string{"status", "timestamp"}},
		{"InfoResponse", NewInfoResponse(testTime), []string{"name", "version", "go_version", "timestamp"}},
		{"EchoResponse", NewEchoResponse("x", testTime), []string{"message", "echoed_at"}},
		{"EchoRequest", EchoRequest{Message: "x"}, []string{"message"}},
		{"LoginRequest", LoginRequest{Username: "u", Password: "p"}, []string{"username", "password"}},
		{"RefreshRequest", RefreshRequest{RefreshToken: "r"}, []string{"refresh_token"}},
		{"TokenPair", NewTokenPair("a", "r", testTime), []string{"access_token", "refresh_token", "token_type", "expires_at"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(tt.value)
			require.NoError(t, err)

			var decoded map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &decoded))

			keys := make([]string, 0, len(decoded))
			for k := range decoded {
				keys = append(keys, k)
			}
			assert.ElementsMatch(t, tt.want, keys)
		})
	}
}

func TestErrorResponseOmitsEmptyFields(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(NewErrorResponse(ErrCodeNotFound, "nope"))
	require.NoError(t, err)

	// omitempty must hold: a client checking for "fields" should not see
	// an empty array on errors that carry no field detail.
	assert.NotContains(t, string(raw), "fields")
	assert.JSONEq(t, `{"code":"NOT_FOUND","message":"nope"}`, string(raw))
}

func TestErrorResponseIncludesFieldDetail(t *testing.T) {
	t.Parallel()

	resp := ErrorResponse{
		Code:    ErrCodeValidationFailed,
		Message: "bad",
		Fields:  []FieldError{{Field: "message", Reason: "is required"}},
	}

	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	assert.JSONEq(t,
		`{"code":"VALIDATION_FAILED","message":"bad","fields":[{"field":"message","reason":"is required"}]}`,
		string(raw))
}

// TestTimestampsSerialiseAsRFC3339 pins the timestamp format, since
// clients parse it.
func TestTimestampsSerialiseAsRFC3339(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(NewHealthResponse(testTime))
	require.NoError(t, err)

	assert.Contains(t, string(raw),
		fmt.Sprintf(`"timestamp":%q`, testTime.Format(time.RFC3339Nano)))
}
