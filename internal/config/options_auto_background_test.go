package config

import (
	"testing"
	"time"

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

func TestOptionsGetJobOutputWait(t *testing.T) {
	t.Parallel()

	value := func(n int) *int { return &n }

	tests := []struct {
		name     string
		options  *Options
		expected time.Duration
	}{
		{name: "nil options", options: nil, expected: DefaultJobOutputWait * time.Second},
		{name: "unset", options: &Options{}, expected: DefaultJobOutputWait * time.Second},
		{name: "zero disables waiting", options: &Options{JobOutputWait: value(0)}, expected: 0},
		{name: "negative falls back to the default", options: &Options{JobOutputWait: value(-1)}, expected: DefaultJobOutputWait * time.Second},
		{name: "configured", options: &Options{JobOutputWait: value(30)}, expected: 30 * time.Second},
		{name: "zero waits nothing", options: &Options{JobOutputWait: value(0)}, expected: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, tt.options.GetJobOutputWait())
		})
	}
}
