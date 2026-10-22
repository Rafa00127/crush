package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTool struct {
	info fantasy.ToolInfo
	opts fantasy.ProviderOptions
	ran  *bool
	arg  *string
}

func (f *fakeTool) Info() fantasy.ToolInfo { return f.info }

func (f *fakeTool) Run(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if f.ran != nil {
		*f.ran = true
	}
	if f.arg != nil {
		*f.arg = call.Input
	}
	return fantasy.NewTextResponse("ok"), nil
}

func (f *fakeTool) ProviderOptions() fantasy.ProviderOptions     { return f.opts }
func (f *fakeTool) SetProviderOptions(o fantasy.ProviderOptions) { f.opts = o }

func mcpFake(name, desc string, props map[string]any, required ...string) *fakeTool {
	return &fakeTool{info: fantasy.ToolInfo{
		Name: name, Description: desc, Parameters: props, Required: required,
	}}
}

func surface() []fantasy.AgentTool {
	return []fantasy.AgentTool{
		NewDeferredTool(mcpFake("mcp__grafana__query_prometheus",
			"Query Prometheus using PromQL. Supports instant and range queries against a datasource.",
			map[string]any{
				"datasourceUid": map[string]any{"type": "string"},
				"expr":          map[string]any{"type": "string"},
			}, "datasourceUid", "expr")),
		NewDeferredTool(mcpFake("mcp__grafana__list_alert_rules",
			"List alert rules configured in Grafana, paginated.",
			map[string]any{"limit": map[string]any{"type": "integer"}})),
		NewDeferredTool(mcpFake("mcp__context7__query-docs",
			"Search library documentation for a topic and return matching snippets.",
			map[string]any{
				"libraryId": map[string]any{"type": "string"},
				"query":     map[string]any{"type": "string"},
			}, "libraryId", "query")),
		NewDeferredTool(mcpFake("mcp__playwright__browser_click",
			"Click an element on the current page.",
			map[string]any{"selector": map[string]any{"type": "string"}})),
	}
}

func TestDeferredToolWithholdsSchemaButStaysCallable(t *testing.T) {
	t.Parallel()

	var ran bool
	var gotArgs string
	inner := mcpFake("mcp__grafana__query_prometheus",
		"Query Prometheus using PromQL. Long paragraphs of parameter documentation follow that we do not want to pay for on every request.",
		map[string]any{"expr": map[string]any{"type": "string"}}, "expr")
	inner.ran, inner.arg = &ran, &gotArgs

	d := NewDeferredTool(inner)
	info := d.Info()

	assert.Equal(t, inner.Info().Name, info.Name)
	assert.Empty(t, info.Parameters, "the schema is what we are withholding")
	assert.Empty(t, info.Required)
	assert.Contains(t, info.Description, "Query Prometheus using PromQL.")
	assert.Contains(t, info.Description, DeferredParamsHint)
	assert.NotContains(t, info.Description, "Long paragraphs",
		"only the first sentence should survive")

	_, err := d.Run(context.Background(), fantasy.ToolCall{
		Name: info.Name, Input: `{"expr":"up"}`,
	})
	require.NoError(t, err)
	assert.True(t, ran, "deferred tool must delegate to the real tool")
	assert.JSONEq(t, `{"expr":"up"}`, gotArgs, "arguments must pass through untouched")
}

func TestWorthDeferringSkipsToolsItCannotShrink(t *testing.T) {
	t.Parallel()

	tiny := mcpFake("mcp__mom__mom_inbox", "Read the inbox.", map[string]any{})
	assert.False(t, WorthDeferring(tiny), "a tool with no schema cannot be shrunk")

	fat := mcpFake("mcp__grafana__query_prometheus",
		"Query Prometheus. "+strings.Repeat("Extensive parameter documentation. ", 20),
		map[string]any{
			"datasourceUid": map[string]any{"type": "string", "description": strings.Repeat("x", 200)},
			"expr":          map[string]any{"type": "string", "description": strings.Repeat("y", 200)},
			"startTime":     map[string]any{"type": "string", "description": strings.Repeat("z", 200)},
		}, "datasourceUid", "expr", "startTime")
	assert.True(t, WorthDeferring(fat), "a real MCP tool with a big schema should defer")
}

func TestDeferringShrinksRealisticSurface(t *testing.T) {
	t.Parallel()

	var eager, deferredBytes, kept int
	for _, tool := range realisticSurface() {
		full, err := json.Marshal(tool.Info())
		require.NoError(t, err)
		eager += len(full)

		if !WorthDeferring(tool) {
			kept++
			deferredBytes += len(full)
			continue
		}
		stub, err := json.Marshal(NewDeferredTool(tool).Info())
		require.NoError(t, err)
		deferredBytes += len(stub)
	}

	assert.Less(t, deferredBytes, eager, "deferring must shrink the request")
	t.Logf("eager %d B vs deferred %d B (%.2fx smaller); %d tool(s) kept eager as not worth deferring",
		eager, deferredBytes, float64(eager)/float64(deferredBytes), kept)
}

func realisticSurface() []fantasy.AgentTool {
	mk := func(name, first string, params int) fantasy.AgentTool {
		props := map[string]any{}
		for i := range params {
			props[string(rune('a'+i))+"Param"] = map[string]any{
				"type":        "string",
				"description": strings.Repeat("parameter documentation ", 8),
			}
		}
		desc := first + " " + strings.Repeat("Further usage notes and caveats. ", 6)
		return mcpFake(name, desc, props)
	}
	return []fantasy.AgentTool{
		mk("mcp__grafana__query_prometheus", "Query Prometheus using PromQL.", 6),
		mk("mcp__grafana__list_alert_rules", "List alert rules.", 4),
		mk("mcp__grafana__query_loki_logs", "Query Loki logs with LogQL.", 6),
		mk("mcp__context7__query-docs", "Search library documentation.", 2),
		mk("mcp__playwright__browser_click", "Click an element.", 3),
	}
}

func TestDeferredToolInfoIsStable(t *testing.T) {
	t.Parallel()

	first, err := json.Marshal(toolInfos(surface()))
	require.NoError(t, err)
	for range 5 {
		again, err := json.Marshal(toolInfos(surface()))
		require.NoError(t, err)
		require.JSONEq(t, string(first), string(again))
	}

	a := NewToolSearchTool(surface(), 5).Info().Description
	shuffled := surface()
	shuffled[0], shuffled[3] = shuffled[3], shuffled[0]
	b := NewToolSearchTool(shuffled, 5).Info().Description
	assert.Equal(t, a, b, "search description must not depend on input order")
}

func toolInfos(ts []fantasy.AgentTool) []fantasy.ToolInfo {
	out := make([]fantasy.ToolInfo, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Info())
	}
	return out
}

func searchOnce(t *testing.T, input string) string {
	t.Helper()
	res, err := NewToolSearchTool(surface(), 5).Run(context.Background(),
		fantasy.ToolCall{Name: ToolSearchToolName, Input: input})
	require.NoError(t, err)
	return res.Content
}

func TestToolSearchFindsToolByIntent(t *testing.T) {
	t.Parallel()

	out := searchOnce(t, `{"query":"query prometheus metrics"}`)
	assert.Contains(t, out, "mcp__grafana__query_prometheus")
	assert.Contains(t, out, "datasourceUid")
	assert.Contains(t, out, "expr")

	idxQuery := strings.Index(out, "mcp__grafana__query_prometheus")
	idxAlert := strings.Index(out, "mcp__grafana__list_alert_rules")
	if idxAlert >= 0 {
		assert.Less(t, idxQuery, idxAlert, "name matches should rank first")
	}

	docs := searchOnce(t, `{"query":"library documentation"}`)
	assert.Contains(t, docs, "mcp__context7__query-docs")
	assert.Contains(t, docs, "libraryId")
}

func TestToolSearchByExactName(t *testing.T) {
	t.Parallel()

	out := searchOnce(t, `{"names":["mcp__playwright__browser_click"]}`)
	assert.Contains(t, out, "mcp__playwright__browser_click")
	assert.Contains(t, out, "selector")

	out = searchOnce(t, `{"names":["mcp__nope__missing"],"query":"click"}`)
	assert.Contains(t, out, "mcp__nope__missing")
}

func TestToolSearchReportsUnknownNames(t *testing.T) {
	t.Parallel()

	out := searchOnce(t, `{"names":["mcp__nope__missing"]}`)
	assert.Contains(t, out, "unknown tool name")
	assert.Contains(t, out, "mcp__nope__missing")
	assert.Contains(t, out, "grafana")
}

func TestToolSearchIsDeterministic(t *testing.T) {
	t.Parallel()

	first := searchOnce(t, `{"query":"grafana"}`)
	for range 4 {
		assert.Equal(t, first, searchOnce(t, `{"query":"grafana"}`))
	}
}

func TestToolSearchRejectsEmptyAndUnmatchedQueries(t *testing.T) {
	t.Parallel()

	out := searchOnce(t, `{}`)
	assert.Contains(t, strings.ToLower(out), "provide a query")

	out = searchOnce(t, `{"query":"zzzznothingmatchesthis"}`)
	assert.Contains(t, strings.ToLower(out), "no tool matched")
	assert.Contains(t, out, "grafana")
}

func TestToolSearchRespectsMaxResults(t *testing.T) {
	t.Parallel()

	res, err := NewToolSearchTool(surface(), 1).Run(context.Background(),
		fantasy.ToolCall{Input: `{"query":"grafana query alert rules docs click"}`})
	require.NoError(t, err)

	var out struct {
		Matches []map[string]any `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
	assert.Len(t, out.Matches, 1)
}

func TestSummarizeKeepsFirstSentence(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Do a thing.", summarize("Do a thing. Then a lot more detail."))
	assert.Equal(t, "(no description provided)", summarize("   "))
	assert.Equal(t, "One two three.", summarize("One\n  two   three."))

	long := summarize(strings.Repeat("word ", 100))
	assert.LessOrEqual(t, len(long), 201)
	assert.True(t, strings.HasSuffix(long, "…"))
}

func TestDeferredToolSelfHealsMissingArgs(t *testing.T) {
	t.Parallel()

	var ran bool
	inner := mcpFake("mcp__grafana__query_prometheus", "Query Prometheus.",
		map[string]any{
			"datasourceUid": map[string]any{"type": "string"},
			"expr":          map[string]any{"type": "string"},
		}, "datasourceUid", "expr")
	inner.ran = &ran
	d := NewDeferredTool(inner)

	res, err := d.Run(context.Background(), fantasy.ToolCall{Input: `{}`})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.False(t, ran, "a call we know is malformed must not reach the server")
	assert.Contains(t, res.Content, "datasourceUid")
	assert.Contains(t, res.Content, "expr")
	assert.Contains(t, res.Content, "\"parameters\"")
	assert.Contains(t, res.Content, "no arguments at all")

	res, err = d.Run(context.Background(), fantasy.ToolCall{Input: `{"expr":"up"}`})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, res.Content, "datasourceUid")
	assert.NotContains(t, res.Content, "missing required parameter(s): expr")
	assert.NotContains(t, res.Content, "no arguments at all")

	res, err = d.Run(context.Background(), fantasy.ToolCall{
		Input: `{"datasourceUid":"prod","expr":"up"}`,
	})
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.True(t, ran)
}

func TestDeferredToolDoesNotSecondGuessTheServer(t *testing.T) {
	t.Parallel()

	var ran bool
	noReq := mcpFake("mcp__mom__mom_board", "Read the board.",
		map[string]any{"channel": map[string]any{"type": "string"}})
	noReq.ran = &ran
	_, err := NewDeferredTool(noReq).Run(context.Background(), fantasy.ToolCall{Input: `{}`})
	require.NoError(t, err)
	assert.True(t, ran)

	ran = false
	withReq := mcpFake("mcp__grafana__query_prometheus", "Query.",
		map[string]any{"expr": map[string]any{"type": "string"}}, "expr")
	withReq.ran = &ran
	_, err = NewDeferredTool(withReq).Run(context.Background(), fantasy.ToolCall{Input: `not json`})
	require.NoError(t, err)
	assert.True(t, ran, "malformed JSON should reach the server, which owns that error")
}

func aliasSurface() []fantasy.AgentTool {
	noParams := map[string]any{"path": map[string]any{"type": "string"}}
	return []fantasy.AgentTool{
		NewDeferredTool(mcpFake("mcp__rea__procedure_pseudo_code",
			"Decompile a procedure and return pseudocode.", noParams)),
		NewDeferredTool(mcpFake("mcp__rea__inspect_go_binary",
			"Inspect Go build info and module versions.", noParams)),
		NewDeferredTool(mcpFake("mcp__rea__inspect_pe_resources",
			"Inspect PE resources and headers.", noParams)),
		NewDeferredTool(mcpFake("mcp__rea__search_strings",
			"Search strings in a binary.", noParams)),
		NewDeferredTool(mcpFake("mcp__google__search",
			"Search the web.", noParams)),
	}
}

func searchTools(t *testing.T, tools []fantasy.AgentTool, input string) string {
	t.Helper()
	res, err := NewToolSearchTool(tools, 5).Run(context.Background(),
		fantasy.ToolCall{Name: ToolSearchToolName, Input: input})
	require.NoError(t, err)
	return res.Content
}

func TestToolSearchChineseAliases(t *testing.T) {
	t.Parallel()

	out := searchTools(t, aliasSurface(), `{"query":"反编译这个函数"}`)
	assert.Contains(t, out, "mcp__rea__procedure_pseudo_code")

	out = searchTools(t, aliasSurface(), `{"query":"字符串"}`)
	assert.Contains(t, out, "mcp__rea__search_strings")
}

func TestToolSearchDoesNotSubstringMatchShortTokens(t *testing.T) {
	t.Parallel()

	out := searchTools(t, aliasSurface(), `{"query":"go"}`)
	assert.Contains(t, out, "mcp__rea__inspect_go_binary")
	assert.NotContains(t, out, "mcp__google__search")
}

func TestToolSearchEnglishAliases(t *testing.T) {
	t.Parallel()

	out := searchTools(t, aliasSurface(), `{"query":"exe resources"}`)
	assert.Contains(t, out, "mcp__rea__inspect_pe_resources")
}
