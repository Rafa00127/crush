package config

import (
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

func TestConfig_resolveWebSearch(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	enabled := true

	cfg := &Config{Options: &Options{WebSearch: &WebSearchOptions{
		Enabled: &enabled,
		URL:     "https://api.deepseek.com/v1/tools/search",
		APIKey:  "$DEEPSEEK_API_KEY",
		Headers: map[string]string{
			"X-Api-Key": "${DEEPSEEK_API_KEY}",
			"X-Literal": "plain",
		},
	}}}

	cfg.resolveWebSearch(NewShellVariableResolver(env.New()))

	ws := cfg.Options.WebSearch
	require.Equal(t, "sk-test", ws.APIKey)
	require.Equal(t, "sk-test", ws.Headers["X-Api-Key"])
	require.Equal(t, "plain", ws.Headers["X-Literal"])
}

func TestConfig_resolveWebSearchNilSafe(t *testing.T) {
	t.Parallel()

	var cfg *Config
	require.NotPanics(t, func() { cfg.resolveWebSearch(IdentityResolver()) })

	cfg = &Config{}
	require.NotPanics(t, func() { cfg.resolveWebSearch(IdentityResolver()) })
}

func TestWebSearchOptions_GetTimeout(t *testing.T) {
	t.Parallel()

	require.Equal(t, 30*time.Second, WebSearchOptions{}.GetTimeout())
	require.Equal(t, 30*time.Second, WebSearchOptions{Timeout: -1}.GetTimeout())
	require.Equal(t, 5*time.Second, WebSearchOptions{Timeout: 5}.GetTimeout())
}

func TestWebSearchOptions_IsEnabled(t *testing.T) {
	t.Parallel()

	enabled := true
	require.False(t, WebSearchOptions{}.IsEnabled())
	require.True(t, WebSearchOptions{Enabled: &enabled}.IsEnabled())
}
