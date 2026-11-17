package config

import (
	"strings"
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

func TestOptionsGetShellEnv(t *testing.T) {
	// Not parallel: t.Setenv forbids it.
	t.Setenv("CRUSH_TEST_BIN", `C:\msys64\usr\bin`)

	opts := &Options{ShellEnv: []string{
		`PATH=$CRUSH_TEST_BIN;$PATH`,
		`MSYSTEM=MSYS`,
		`BARE$NOT_A_REF=x`,
		`=ignored`,
	}}
	got := opts.GetShellEnv()

	require.Len(t, got, 3, "entries without a key are dropped")
	require.True(t, strings.HasPrefix(got[0], `PATH=C:\msys64\usr\bin;`),
		"the leading reference expands and the rest of PATH follows: %q", got[0])
	require.NotContains(t, got[0], "$CRUSH_TEST_BIN")
	require.Equal(t, "MSYSTEM=MSYS", got[1])
}

func TestOptionsGetBashPath(t *testing.T) {
	t.Parallel()

	require.Empty(t, (*Options)(nil).GetBashPath())
	require.Empty(t, (&Options{}).GetBashPath())
	require.Equal(t, `C:/msys64/usr/bin/bash.exe`,
		(&Options{BashPath: " C:/msys64/usr/bin/bash.exe "}).GetBashPath())
}
