package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

func TestProviderSearchFromConfig(t *testing.T) {
	t.Parallel()

	providers := func() *csync.Map[string, config.ProviderConfig] {
		m := csync.NewMap[string, config.ProviderConfig]()
		m.Set("deepseek", config.ProviderConfig{
			ID:      "deepseek",
			BaseURL: "https://api.deepseek.com/",
			APIKey:  "provider-key",
		})
		return m
	}

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Providers: providers(),
			Options:   &config.Options{WebSearch: &config.WebSearchOptions{Provider: "deepseek"}},
		}
		require.Nil(t, ProviderSearchFromConfig(cfg))
	})

	t.Run("no options", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, ProviderSearchFromConfig(&config.Config{Options: &config.Options{}}))
		require.Nil(t, ProviderSearchFromConfig(nil))
	})

	t.Run("borrows provider credentials", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Providers: providers(),
			Options: &config.Options{WebSearch: &config.WebSearchOptions{
				Enabled:  boolPtr(true),
				Provider: "deepseek",
			}},
		}

		got := ProviderSearchFromConfig(cfg)
		require.NotNil(t, got)
		require.Equal(t, "https://api.deepseek.com/v1/tools/search", got.URL)
		require.Equal(t, "provider-key", got.APIKey)
		require.Equal(t, defaultProviderSearchCount, got.Count)
		require.Equal(t, defaultProviderSearchTimeout, got.Timeout)
		require.False(t, got.NeedContent)
		require.Equal(t, "deepseek", got.Backend)
	})

	t.Run("explicit url and key win", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Providers: providers(),
			Options: &config.Options{WebSearch: &config.WebSearchOptions{
				Enabled:  boolPtr(true),
				Provider: "deepseek",
				URL:      "https://search.example.com/api",
				APIKey:   "explicit-key",
				Count:    3,
				Headers:  map[string]string{"X-Tenant": "acme"},
			}},
		}

		got := ProviderSearchFromConfig(cfg)
		require.NotNil(t, got)
		require.Equal(t, "https://search.example.com/api", got.URL)
		require.Equal(t, "explicit-key", got.APIKey)
		require.Equal(t, 3, got.Count)
		require.Equal(t, "acme", got.Headers["X-Tenant"])
	})

	t.Run("custom path", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Providers: providers(),
			Options: &config.Options{WebSearch: &config.WebSearchOptions{
				Enabled:  boolPtr(true),
				Provider: "deepseek",
				Path:     "tools/search",
			}},
		}

		got := ProviderSearchFromConfig(cfg)
		require.NotNil(t, got)
		require.Equal(t, "https://api.deepseek.com/tools/search", got.URL)
	})

	t.Run("unresolvable endpoint", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Providers: providers(),
			Options: &config.Options{WebSearch: &config.WebSearchOptions{
				Enabled:  boolPtr(true),
				Provider: "missing",
			}},
		}
		require.Nil(t, ProviderSearchFromConfig(cfg))
	})
}

func TestSearchProviderAPI(t *testing.T) {
	t.Parallel()

	var (
		gotPath    string
		gotAuth    string
		gotTenant  string
		gotRequest providerSearchRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotTenant = r.Header.Get("X-Tenant")
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &gotRequest))

		_, _ = w.Write([]byte(`{
			"results": [
				{
					"rerank_score": 2.4,
					"title": "DeepSeek V4",
					"url": "https://example.com/v4",
					"snippet": "short snippet",
					"summary": "long model-facing summary",
					"content": "page body",
					"date": "2026-06-01T00:00:00+00:00"
				},
				{
					"title": "No summary here",
					"url": "https://example.com/other",
					"snippet": "fallback snippet"
				}
			]
		}`))
	}))
	defer server.Close()

	cfg := ProviderSearchConfig{
		URL:         server.URL + "/v1/tools/search",
		APIKey:      "secret",
		Headers:     map[string]string{"X-Tenant": "acme"},
		Count:       20,
		NeedContent: true,
		Timeout:     5 * time.Second,
	}

	results, err := searchProviderAPI(t.Context(), server.Client(), cfg, "deepseek v4", 5)
	require.NoError(t, err)

	require.Equal(t, "/v1/tools/search", gotPath)
	require.Equal(t, "Bearer secret", gotAuth)
	require.Equal(t, "acme", gotTenant)
	require.Equal(t, "deepseek v4", gotRequest.Query)
	require.Equal(t, 5, gotRequest.Count, "max_results must cap the requested count")
	require.True(t, gotRequest.NeedContent)

	require.Len(t, results, 2)
	require.Equal(t, "DeepSeek V4", results[0].Title)
	require.Equal(t, "https://example.com/v4", results[0].Link)
	require.Equal(t, "long model-facing summary", results[0].Snippet)
	require.Equal(t, 1, results[0].Position)
	require.Equal(t, "fallback snippet", results[1].Snippet)
	require.Equal(t, 2, results[1].Position)
}

func TestSearchProviderAPIEmptyResults(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results": []}`))
	}))
	defer server.Close()

	results, err := searchProviderAPI(t.Context(), server.Client(), ProviderSearchConfig{URL: server.URL}, "nothing", 0)
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestSearchProviderAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key","type":"invalid_request_error","code":"invalid_request_error"}}`))
	}))
	defer server.Close()

	_, err := searchProviderAPI(t.Context(), server.Client(), ProviderSearchConfig{URL: server.URL}, "q", 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "invalid api key")
}

func TestWebSearchDescriptionNamesBackend(t *testing.T) {
	t.Parallel()

	require.Contains(t, webSearchDescription(nil), "DuckDuckGo")
	require.Contains(t, webSearchDescription(&ProviderSearchConfig{Backend: "deepseek"}), "deepseek")
	require.NotContains(t, webSearchDescription(&ProviderSearchConfig{Backend: "deepseek"}), "DuckDuckGo")
}

func TestTruncateSnippetCapsLength(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", maxProviderSearchSnippet+50)
	got := truncateSnippet(long)
	require.Len(t, got, maxProviderSearchSnippet+3)
	require.True(t, strings.HasSuffix(got, "..."))
	require.Empty(t, truncateSnippet("  ", ""))
}

func TestEffectiveMaxResults(t *testing.T) {
	t.Parallel()

	provider := &ProviderSearchConfig{URL: "https://search.example.com", Count: 50}

	t.Run("scraper keeps default and 20 cap", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, 10, effectiveMaxResults(0, nil))
		require.Equal(t, 5, effectiveMaxResults(5, nil))
		require.Equal(t, 20, effectiveMaxResults(50, nil))
	})

	t.Run("provider uses configured count", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, 50, effectiveMaxResults(0, provider))
		require.Equal(t, 25, effectiveMaxResults(25, provider))
		require.Equal(t, 50, effectiveMaxResults(80, provider))
		require.False(t, hasProviderSearch(&ProviderSearchConfig{}))
	})
}

func TestProviderSearchBackendNamesHost(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: csync.NewMap[string, config.ProviderConfig](),
		Options: &config.Options{WebSearch: &config.WebSearchOptions{
			Enabled: boolPtr(true),
			URL:     "https://api.deepseek.com/v1/tools/search",
		}},
	}

	got := ProviderSearchFromConfig(cfg)
	require.NotNil(t, got)
	require.Equal(t, "api.deepseek.com", got.Backend)
}
