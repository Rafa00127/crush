package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The mode flag is persistent so `crush run --mode mini` selects the same
// toolset as the interactive command.
func TestModeFlagAvailableOnRunCmd(t *testing.T) {
	t.Parallel()

	flag := runCmd.Flags().Lookup("mode")
	require.NotNil(t, flag, "the --mode flag must be available on `crush run` (register it on rootCmd as persistent)")
	require.Equal(t, "", flag.DefValue, "the default is the full toolset")
}
