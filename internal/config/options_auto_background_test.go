package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptionsGetAutoBackgroundAfter(t *testing.T) {
	t.Parallel()

	value := func(n int) *int { return &n }

	tests := []struct {
		name     string
		options  *Options
		expected int
	}{
		{name: "nil options", options: nil, expected: DefaultAutoBackgroundAfter},
		{name: "unset", options: &Options{}, expected: DefaultAutoBackgroundAfter},
		{name: "zero restores the default", options: &Options{AutoBackgroundAfter: value(0)}, expected: DefaultAutoBackgroundAfter},
		{name: "negative restores the default", options: &Options{AutoBackgroundAfter: value(-1)}, expected: DefaultAutoBackgroundAfter},
		{name: "configured", options: &Options{AutoBackgroundAfter: value(5)}, expected: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, tt.options.GetAutoBackgroundAfter())
		})
	}
}
