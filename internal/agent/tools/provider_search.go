package tools

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/config"
)

const (
	// DefaultProviderSearchPath is the search path appended to a provider
	// base URL. It matches DeepSeek's tools/search endpoint.
	DefaultProviderSearchPath = "/v1/tools/search"
	// defaultProviderSearchCount is how many results to request per search
	// when the config leaves count unset.
	defaultProviderSearchCount = 10
	// defaultProviderSearchTimeout bounds one search request.
	defaultProviderSearchTimeout = 30 * time.Second
	// maxProviderSearchSnippet caps the text carried back per result so one
	// verbose endpoint cannot flood the context.
	maxProviderSearchSnippet = 1200
	// maxProviderSearchBody caps how much of an error or success body is
	// read, keeping a hostile endpoint from exhausting memory.
	maxProviderSearchBody = 4 << 20
)

// ProviderSearchConfig is the resolved provider-hosted search endpoint the
// web_search tool calls. A nil value means the built-in DuckDuckGo scraper.
type ProviderSearchConfig struct {
	// URL is the search endpoint that receives POST requests.
	URL string
	// APIKey is sent as a bearer token when non-empty.
	APIKey string
	// Headers are extra request headers from configuration.
	Headers map[string]string
	// Count is the preferred number of results per search.
	Count int
	// NeedContent asks the endpoint for full page content too.
	NeedContent bool
	// Timeout bounds a single request.
	Timeout time.Duration
	// Backend names the backend for tool descriptions ("DeepSeek", a host,
	// ...). It is display only.
	Backend string
}

// ProviderSearchFromConfig resolves options.web_search against the provider
// it borrows credentials from. It returns nil when the backend is off or when
// no endpoint can be resolved, so callers fall back to DuckDuckGo.
func ProviderSearchFromConfig(cfg *config.Config) *ProviderSearchConfig {
	if cfg == nil || cfg.Options.WebSearch == nil || !cfg.Options.WebSearch.IsEnabled() {
		return nil
	}
	opts := cfg.Options.WebSearch

	var baseURL, apiKey string
	if opts.Provider != "" {
		if cfg.Providers != nil {
			if provider, ok := cfg.Providers.Get(opts.Provider); ok {
				baseURL = provider.BaseURL
				apiKey = provider.APIKey
			}
		}
		if baseURL == "" {
			slog.Warn("Web search provider is not configured", "provider", opts.Provider)
		}
	}

	endpoint := strings.TrimSpace(opts.URL)
	if endpoint == "" {
		endpoint = joinSearchURL(baseURL, cmp.Or(opts.Path, DefaultProviderSearchPath))
	}
	if endpoint == "" {
		slog.Warn("Web search is enabled but has no resolvable endpoint",
			"provider", opts.Provider, "url", opts.URL)
		return nil
	}

	if key := strings.TrimSpace(opts.APIKey); key != "" {
		apiKey = key
	}

	backend := opts.Provider
	if backend == "" {
		if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
			backend = parsed.Host
		} else {
			backend = endpoint
		}
	}

	return &ProviderSearchConfig{
		URL:         endpoint,
		APIKey:      apiKey,
		Headers:     opts.Headers,
		Count:       cmp.Or(opts.Count, defaultProviderSearchCount),
		NeedContent: opts.NeedContent,
		Timeout:     opts.GetTimeout(),
		Backend:     backend,
	}
}

// joinSearchURL appends a search path to a provider base URL, tolerating a
// trailing slash on either side.
func joinSearchURL(baseURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// providerSearchRequest is the request body accepted by DeepSeek's
// tools/search and by endpoints that follow the same shape.
type providerSearchRequest struct {
	Query       string `json:"query"`
	Count       int    `json:"count,omitempty"`
	NeedContent bool   `json:"need_content,omitempty"`
}

// providerSearchResponse is the documented response envelope: results may be
// empty, never null, for a search with no matches.
type providerSearchResponse struct {
	Results []providerSearchResult `json:"results"`
}

type providerSearchResult struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Snippet  string `json:"snippet"`
	Summary  string `json:"summary"`
	Content  string `json:"content"`
	SiteName string `json:"site_name"`
	Date     string `json:"date"`
}

// providerSearchError is the OpenAI-style error envelope these endpoints
// return on failure.
type providerSearchError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// searchProviderAPI runs one search against the configured endpoint and maps
// its results into the shape the tool renders.
func searchProviderAPI(ctx context.Context, client *http.Client, cfg ProviderSearchConfig, query string, maxResults int) ([]SearchResult, error) {
	count := cfg.Count
	if count <= 0 {
		count = defaultProviderSearchCount
	}
	if maxResults > 0 && maxResults < count {
		count = maxResults
	}

	payload, err := json.Marshal(providerSearchRequest{
		Query:       query,
		Count:       count,
		NeedContent: cfg.NeedContent,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode search request: %w", err)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultProviderSearchTimeout
	}
	searchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(searchCtx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}

	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute search: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderSearchBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read search response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search failed with status code %d: %s", resp.StatusCode, providerErrorMessage(body))
	}

	var decoded providerSearchResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	return toSearchResults(decoded.Results, maxResults), nil
}

// providerErrorMessage extracts the message from an error envelope, falling
// back to a trimmed body so a plain-text error page still says something.
func providerErrorMessage(body []byte) string {
	var envelope providerSearchError
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return envelope.Error.Message
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 300 {
		text = text[:300] + "..."
	}
	if text == "" {
		return "no response body"
	}
	return text
}

// toSearchResults maps endpoint results onto the renderer's result type,
// preferring the long summary the endpoint writes for models and falling back
// to the short snippet and then the page content.
func toSearchResults(results []providerSearchResult, maxResults int) []SearchResult {
	out := make([]SearchResult, 0, len(results))
	for _, result := range results {
		if result.URL == "" && result.Title == "" {
			continue
		}
		out = append(out, SearchResult{
			Title:    strings.TrimSpace(result.Title),
			Link:     result.URL,
			Snippet:  truncateSnippet(result.Summary, result.Snippet, result.Content),
			Position: len(out) + 1,
		})
		if maxResults > 0 && len(out) >= maxResults {
			break
		}
	}
	return out
}

func truncateSnippet(candidates ...string) string {
	for _, candidate := range candidates {
		text := strings.TrimSpace(candidate)
		if text == "" {
			continue
		}
		text = strings.Join(strings.Fields(text), " ")
		if len(text) > maxProviderSearchSnippet {
			text = text[:maxProviderSearchSnippet] + "..."
		}
		return text
	}
	return ""
}
