package tools

import (
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
)

// ServerWebSearchMaxUses is the default cap on server-side web searches
// the model may perform within a single request. It maps to Anthropic's
// `max_uses`, which bounds both cost and latency.
const ServerWebSearchMaxUses = 5

// NewAnthropicWebSearchTool returns Anthropic's provider-defined
// `web_search` tool. The search runs server-side: the provider executes
// it and returns citeable sources, so no local HTTP client is involved.
//
// This is only valid for providers speaking the Anthropic Messages API
// (`anthropic.Name`). Other provider types ignore provider-defined tools,
// which is why callers must check the provider type before using it.
func NewAnthropicWebSearchTool() fantasy.ProviderTool {
	return anthropic.WebSearchTool(&anthropic.WebSearchToolOptions{
		MaxUses: ServerWebSearchMaxUses,
	})
}
