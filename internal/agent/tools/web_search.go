package tools

import (
	"context"
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"charm.land/fantasy"
)

//go:embed web_search.md.tpl
var webSearchDescriptionTmpl []byte

var webSearchDescriptionTpl = template.Must(
	template.New("webSearchDescription").
		Parse(string(webSearchDescriptionTmpl)),
)

// webSearchDescriptionData feeds the tool description template.
type webSearchDescriptionData struct {
	GhAvailable bool
	// Backend names the provider-hosted search endpoint when one is
	// configured; empty means the built-in DuckDuckGo scraper.
	Backend string
}

func webSearchDescription(providerSearch *ProviderSearchConfig) string {
	var backend string
	if providerSearch != nil {
		backend = providerSearch.Backend
	}
	return renderTemplate(webSearchDescriptionTpl, webSearchDescriptionData{
		GhAvailable: ghAvailable,
		Backend:     backend,
	})
}

// NewWebSearchTool creates a web search tool for sub-agents (no permissions
// needed). A configured providerSearch replaces the DuckDuckGo scraper.
func NewWebSearchTool(client *http.Client, providerSearch *ProviderSearchConfig) fantasy.AgentTool {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 10
		transport.IdleConnTimeout = 90 * time.Second

		client = &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		}
	}

	return fantasy.NewParallelAgentTool(
		WebSearchToolName,
		webSearchDescription(providerSearch),
		func(ctx context.Context, params WebSearchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Query == "" {
				return fantasy.NewTextErrorResponse("query is required"), nil
			}

			maxResults := effectiveMaxResults(params.MaxResults, providerSearch)

			var results []SearchResult
			var err error
			if hasProviderSearch(providerSearch) {
				results, err = searchProviderAPI(ctx, client, *providerSearch, params.Query, maxResults)
			} else {
				maybeDelaySearch()
				results, err = searchDuckDuckGo(ctx, client, params.Query, maxResults)
			}
			slog.Debug("Web search completed", "query", params.Query, "results", len(results), "err", err)
			if err != nil {
				return fantasy.NewTextErrorResponse("Failed to search: " + err.Error()), nil
			}

			return fantasy.NewTextResponse(formatSearchResults(results)), nil
		},
	)
}

// hasProviderSearch reports whether a provider-hosted endpoint will serve the
// search instead of the built-in scraper.
func hasProviderSearch(providerSearch *ProviderSearchConfig) bool {
	return providerSearch != nil && providerSearch.URL != ""
}

// effectiveMaxResults turns the tool argument into the number of results to
// request. Without a configured endpoint the scraper keeps its historical
// default of 10 and hard cap of 20; a provider endpoint serves the configured
// count (up to that endpoint's own limit), so `count` is the real ceiling
// rather than being clipped at 20.
func effectiveMaxResults(requested int, providerSearch *ProviderSearchConfig) int {
	if !hasProviderSearch(providerSearch) {
		if requested <= 0 {
			return 10
		}
		return min(requested, 20)
	}
	if requested <= 0 {
		if providerSearch.Count > 0 {
			return providerSearch.Count
		}
		return defaultProviderSearchCount
	}
	if providerSearch.Count > 0 {
		return min(requested, providerSearch.Count)
	}
	return requested
}
