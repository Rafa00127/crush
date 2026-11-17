package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A reference inside shell_env is expanded from the parent environment, in
// both the $VAR and %VAR% spellings, and the rest of the value survives. A
// PATH entry is prepended to the inherited one, never a replacement.
func TestOptionsGetShellEnvExpandsReferences(t *testing.T) {
	// Not parallel: t.Setenv forbids it.
	t.Setenv("CRUSH_TEST_DIR", `C:\msys64\usr\bin`)

	opts := &Options{ShellEnv: []string{
		`PATH=$CRUSH_TEST_DIR;D:\other;$PATH`,
		`FROM_PERCENT=%CRUSH_TEST_DIR%\mingw64\bin`,
	}}
	got := opts.GetShellEnv()
	require.Len(t, got, 2)

	require.True(t, strings.HasPrefix(got[0], `PATH=C:\msys64\usr\bin;D:\other`),
		"the directories are prepended in order: %q", got[0])
	require.NotContains(t, got[0], "$CRUSH_TEST_DIR")
	require.NotContains(t, got[0], "$PATH", "a PATH reference must not survive as a literal")
	require.Equal(t, 1, strings.Count(got[0], `C:\msys64\usr\bin`),
		"the inherited PATH is appended once, whatever the value spells")

	require.Equal(t, `FROM_PERCENT=C:\msys64\usr\bin\mingw64\bin`, got[1])
}
