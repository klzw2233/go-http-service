package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPort(t *testing.T) {
	tests := []struct {
		name string
		env  string
		set  bool
		want string
	}{
		{
			name: "未设置时回退到默认端口",
			set:  false,
			want: defaultPort,
		},
		{
			name: "设置后覆盖默认值",
			env:  "9090",
			set:  true,
			want: "9090",
		},
		{
			name: "设置为空串时视作未设置",
			env:  "",
			set:  true,
			want: defaultPort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv handles restoring the previous value, but it
			// forbids t.Parallel, which is why these run serially.
			if tt.set {
				t.Setenv("PORT", tt.env)
			} else {
				// Ensure a PORT inherited from the shell cannot make this
				// case pass or fail by accident.
				t.Setenv("PORT", "")
			}

			assert.Equal(t, tt.want, port())
		})
	}
}
