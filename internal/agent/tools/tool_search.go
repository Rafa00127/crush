package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"charm.land/fantasy"
)

const ToolSearchToolName = "tool_search"

// DeferredParamsHint marks a stub description as missing its parameter schema.
// It is deliberately terse: it is repeated once per deferred tool, so a full
// sentence here would cost more than some tools' schemas do. The convention is
// explained once, in the tool_search tool's own description.
const DeferredParamsHint = "[params: tool_search]"

// deferredTool presents a tool by name and one-line summary while withholding
// its parameter schema, which is where nearly all the bytes live. The real
// schema is served on demand by the tool_search tool.
//
// The stub is deliberately immutable for the lifetime of the process: a
// "loaded" flag that swapped in the full schema after a search would change the
// tools block between turns, and since tools render before system and messages,
// that invalidates the entire prompt cache on every discovery. Serving schemas
// as tool *results* keeps them in message content, after every cache
// breakpoint, so discovery costs one round-trip instead of the whole prefix.
type deferredTool struct {
	inner   fantasy.AgentTool
	summary string
}

// NewDeferredTool wraps a tool so only its name and a one-line summary are sent.
func NewDeferredTool(inner fantasy.AgentTool) fantasy.AgentTool {
	return &deferredTool{
		inner:   inner,
		summary: summarize(inner.Info().Description),
	}
}

// WorthDeferring reports whether withholding a tool's schema actually shrinks
// the request. A tool with a terse description and one or two parameters can be
// smaller sent whole than as a stub, and deferring it would buy a round-trip for
// nothing.
func WorthDeferring(t fantasy.AgentTool) bool {
	full, err := json.Marshal(t.Info())
	if err != nil {
		return false
	}
	stub, err := json.Marshal(NewDeferredTool(t).Info())
	if err != nil {
		return false
	}
	return len(stub) < len(full)
}

func (d *deferredTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{
		Name:        d.inner.Info().Name,
		Description: d.summary + " " + DeferredParamsHint,
		// An empty object schema keeps the tool callable: the model supplies
		// arguments it learned from tool_search, and the MCP server remains the
		// authority on validating them.
		Parameters: map[string]any{},
		Required:   []string{},
		Parallel:   d.inner.Info().Parallel,
	}
}

// Run delegates to the real tool, but first catches the failure mode deferral
// creates: because the stub advertises no parameters, a model may call the tool
// optimistically without loading its schema. Rather than forwarding a call we
// know is malformed, where the server's own error may not hint that a schema is
// available, answer with the schema itself, so one round-trip fixes it
// regardless of how the upstream server words its errors.
func (d *deferredTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if missing := d.missingRequired(call.Input); len(missing) > 0 {
		return fantasy.NewTextErrorResponse(d.schemaHint(missing, call.Input)), nil
	}
	return d.inner.Run(ctx, call)
}

// missingRequired reports which of the tool's required parameters the call omits.
func (d *deferredTool) missingRequired(input string) []string {
	required := d.inner.Info().Required
	if len(required) == 0 {
		return nil
	}
	got := map[string]any{}
	if strings.TrimSpace(input) != "" {
		if err := json.Unmarshal([]byte(input), &got); err != nil {
			// Unparseable input is the server's to reject; only absent required
			// parameters are diagnosed here.
			return nil
		}
	}
	var missing []string
	for _, name := range required {
		v, ok := got[name]
		if !ok || v == nil || v == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

// schemaHint is the corrective response: what was missing, plus the full schema.
func (d *deferredTool) schemaHint(missing []string, rawInput string) string {
	info := d.inner.Info()
	var b strings.Builder
	trimmed := strings.TrimSpace(rawInput)
	if trimmed == "" || trimmed == "{}" {
		b.WriteString("You called this tool with no arguments at all (empty input). ")
		fmt.Fprintf(&b, "It requires: %s.\n\n", strings.Join(missing, ", "))
	} else {
		fmt.Fprintf(&b, "This call is missing required parameter(s): %s.\n\n",
			strings.Join(missing, ", "))
	}
	b.WriteString("Here is the tool's full schema; retry the call with it. ")
	b.WriteString("You do not need to call tool_search for this tool.\n\n")
	payload, err := json.MarshalIndent(toolSearchHit{
		Name:        info.Name,
		Description: info.Description,
		Parameters:  info.Parameters,
		Required:    info.Required,
	}, "", "  ")
	if err != nil {
		fmt.Fprintf(&b, "required: %s", strings.Join(info.Required, ", "))
		return b.String()
	}
	b.Write(payload)
	return b.String()
}

func (d *deferredTool) ProviderOptions() fantasy.ProviderOptions {
	return d.inner.ProviderOptions()
}

func (d *deferredTool) SetProviderOptions(o fantasy.ProviderOptions) {
	d.inner.SetProviderOptions(o)
}

// Unwrap exposes the underlying tool so callers that need the real schema
// (notably tool_search) can reach it.
func (d *deferredTool) Unwrap() fantasy.AgentTool { return d.inner }

// summarize reduces a tool description to its first sentence, bounded, so the
// stub stays cheap while remaining recognizable.
func summarize(desc string) string {
	desc = strings.TrimSpace(strings.ReplaceAll(desc, "\n", " "))
	for strings.Contains(desc, "  ") {
		desc = strings.ReplaceAll(desc, "  ", " ")
	}
	if desc == "" {
		return "(no description provided)"
	}
	if i := strings.IndexAny(desc, ".!?"); i > 0 && i < 200 {
		desc = desc[:i+1]
	}
	const maxLen = 200
	const ellipsis = "…"
	if len(desc) > maxLen {
		budget := maxLen - len(ellipsis)
		cut := strings.LastIndex(desc[:budget], " ")
		if cut <= 0 {
			cut = budget
		}
		desc = desc[:cut] + ellipsis
	}
	return desc
}

type ToolSearchParams struct {
	Query string   `json:"query" description:"What you want to do, in a few words (e.g. \"query prometheus metrics\", \"read a Jira issue\"). Matched against tool names and descriptions."`
	Names []string `json:"names,omitempty" description:"Optional. Exact tool names to load, when you already know them. Skips ranking."`
}

type toolSearchTool struct {
	deferred        []fantasy.AgentTool
	maxResults      int
	providerOptions fantasy.ProviderOptions
}

// NewToolSearchTool builds the discovery tool over a set of deferred tools.
// The slice is sorted by name so the tool's own description, which lists the
// servers it covers, is byte-stable across rebuilds.
func NewToolSearchTool(deferred []fantasy.AgentTool, maxResults int) fantasy.AgentTool {
	if maxResults <= 0 {
		maxResults = 5
	}
	sorted := make([]fantasy.AgentTool, len(deferred))
	copy(sorted, deferred)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Info().Name < sorted[j].Info().Name
	})
	return &toolSearchTool{deferred: sorted, maxResults: maxResults}
}

func (t *toolSearchTool) SetProviderOptions(o fantasy.ProviderOptions) { t.providerOptions = o }
func (t *toolSearchTool) ProviderOptions() fantasy.ProviderOptions     { return t.providerOptions }

func (t *toolSearchTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{
		Name:        ToolSearchToolName,
		Description: t.description(),
		Parameters: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "What you want to do, in a few words. Matched against tool names and descriptions.",
			},
			"names": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional. Exact tool names to load when you already know them.",
			},
		},
		Required: []string{},
	}
}

func (t *toolSearchTool) description() string {
	var b strings.Builder
	b.WriteString("Load the full parameter schema for a tool that was sent without one.\n\n")
	fmt.Fprintf(&b, "Tools marked %s in their description have no parameter schema in this request. ",
		DeferredParamsHint)
	fmt.Fprintf(&b, "There are %d of them", len(t.deferred))
	if servers := t.servers(); len(servers) > 0 {
		fmt.Fprintf(&b, ", from: %s", strings.Join(servers, ", "))
	}
	b.WriteString(".\n\nCall this first for any such tool, then call the tool itself. ")
	b.WriteString("Search by intent (\"query prometheus\", \"list alert rules\") or pass exact names in `names`. ")
	b.WriteString("The result gives each match's full description and JSON schema.")
	return b.String()
}

func (t *toolSearchTool) servers() []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range t.deferred {
		name := d.Info().Name
		rest, ok := strings.CutPrefix(name, "mcp__")
		if !ok {
			continue
		}
		server, _, _ := strings.Cut(rest, "__")
		if server != "" && !seen[server] {
			seen[server] = true
			out = append(out, server)
		}
	}
	sort.Strings(out)
	return out
}

type toolSearchHit struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Required    []string       `json:"required,omitempty"`
}

func (t *toolSearchTool) Run(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var params ToolSearchParams
	if call.Input != "" {
		if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("invalid parameters: %s", err)), nil
		}
	}
	if strings.TrimSpace(params.Query) == "" && len(params.Names) == 0 {
		return fantasy.NewTextErrorResponse(
			"provide a query describing what you want to do, or exact tool names to load",
		), nil
	}

	var hits []fantasy.AgentTool
	var unknown []string
	if len(params.Names) > 0 {
		byName := make(map[string]fantasy.AgentTool, len(t.deferred))
		for _, d := range t.deferred {
			byName[d.Info().Name] = d
		}
		for _, n := range params.Names {
			n = strings.TrimSpace(n)
			if d, ok := byName[n]; ok {
				hits = append(hits, d)
			} else {
				unknown = append(unknown, n)
			}
		}
	}
	if len(hits) == 0 {
		hits = t.rank(params.Query)
	}
	if len(hits) == 0 {
		if len(unknown) > 0 && strings.TrimSpace(params.Query) == "" {
			return fantasy.NewTextErrorResponse(fmt.Sprintf(
				"unknown tool name(s): %s. Available servers: %s. Use a query to search by intent.",
				strings.Join(unknown, ", "), strings.Join(t.servers(), ", "),
			)), nil
		}
		return fantasy.NewTextErrorResponse(fmt.Sprintf(
			"no tool matched %q. Available servers: %s",
			params.Query, strings.Join(t.servers(), ", "),
		)), nil
	}

	out := struct {
		Matches []toolSearchHit `json:"matches"`
		Unknown []string        `json:"unknown_names,omitempty"`
		Note    string          `json:"note"`
	}{
		Unknown: unknown,
		Note:    "Call the tool by name with these parameters. Do not call tool_search again for the same tool.",
	}
	for _, d := range hits {
		info := d.Info()
		if u, ok := d.(interface{ Unwrap() fantasy.AgentTool }); ok {
			info = u.Unwrap().Info()
		}
		out.Matches = append(out.Matches, toolSearchHit{
			Name:        info.Name,
			Description: info.Description,
			Parameters:  info.Parameters,
			Required:    info.Required,
		})
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fantasy.NewTextErrorResponse("could not encode matches"), nil
	}
	return fantasy.NewTextResponse(string(b)), nil
}

// rank scores deferred tools against a free-text query. Scoring is a small
// lexical overlap heuristic rather than real BM25: with a few hundred tools the
// discriminating signal is almost entirely whether query terms appear in the
// tool name, and a heuristic keeps this dependency-free and deterministic.
func (t *toolSearchTool) rank(query string) []fantasy.AgentTool {
	terms := expandTerms(tokenize(query))
	if len(terms) == 0 {
		return nil
	}

	type scored struct {
		tool  fantasy.AgentTool
		score int
		name  string
	}
	var all []scored
	for _, d := range t.deferred {
		info := d.Info()
		full := info
		if u, ok := d.(interface{ Unwrap() fantasy.AgentTool }); ok {
			full = u.Unwrap().Info()
		}
		nameTokens := tokenize(full.Name)
		descTokens := tokenize(full.Description)
		score := 0
		for _, term := range terms {
			if hasTerm(nameTokens, term) {
				score += 8
			}
			if hasTerm(descTokens, term) {
				score += 2
			}
		}
		if score > 0 {
			all = append(all, scored{tool: d, score: score, name: full.Name})
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].name < all[j].name
	})
	if len(all) > t.maxResults {
		all = all[:t.maxResults]
	}
	out := make([]fantasy.AgentTool, 0, len(all))
	for _, s := range all {
		out = append(out, s.tool)
	}
	return out
}

var toolSearchStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "the": true, "to": true, "of": true,
	"for": true, "in": true, "on": true, "with": true, "from": true, "by": true,
	"is": true, "it": true, "this": true, "that": true, "tool": true, "use": true,
	"using": true, "get": true, "me": true, "my": true, "i": true, "want": true,
}

func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := map[string]bool{}
	var out []string
	for _, f := range fields {
		if len(f) < 2 || toolSearchStopWords[f] || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// queryAliases bridges the vocabulary gap between how people ask and how MCP
// tools are named. It is deliberately small and static: deterministic ranking
// matters more than exhaustive coverage, and a model that already knows the
// exact tool name can pass `names` instead.
var queryAliases = map[string][]string{
	"反编译": {"decompile", "decompiler"},
	"反汇编": {"disassemble", "assembly"},
	"伪代码": {"pseudocode"},
	"字符串": {"strings", "string"},
	"函数":  {"function", "procedure"},
	"过程":  {"procedure"},
	"引用":  {"xrefs", "references"},
	"调用":  {"call", "callers", "callees"},
	"调用者": {"callers"},
	"被调用": {"callees"},
	"固件":  {"firmware"},
	"安卓":  {"android"},
	"苹果":  {"apple", "ios"},
	"浏览器": {"browser"},
	"进程":  {"process"},
	"附加":  {"attach"},
	"注入":  {"inject", "instrument"},
	"证据":  {"evidence"},
	"导出":  {"export", "exports"},
	"导入":  {"import", "imports"},
	"符号":  {"symbol", "symbols"},
	"段":   {"section", "segment"},
	"资源":  {"resource", "resources"},
	"清单":  {"manifest"},
	"版本":  {"version"},
	"编译":  {"compiler", "build"},
	"模块":  {"module"},
	"依赖":  {"dependency"},
	"二进制": {"binary"},
	"调试":  {"debug"},
	"断点":  {"breakpoint"},
	"内存":  {"memory"},
	"注册表": {"registry"},
	"驱动":  {"driver"},
	"内核":  {"kernel"},
	"证书":  {"certificate"},
	"签名":  {"signature"},
	"哈希":  {"hash", "digest"},
	"文件头": {"header"},
	"目录":  {"directory"},
	"路径":  {"path"},
	"日志":  {"log"},
	"配置":  {"config"},
	"网络":  {"network"},
	"截图":  {"screenshot"},
	"界面":  {"ui", "interface"},
	"数据库": {"database", "sqlite"},
	"壳":   {"packer", "unpack"},
	"脱壳":  {"unpack"},
	"漏洞":  {"vulnerability"},
	"提权":  {"privilege"},
	"加密":  {"crypto", "encrypt"},
	"时间":  {"time"},

	"disasm":    {"disassemble", "assembly"},
	"decomp":    {"decompile", "decompiler"},
	"pseudo":    {"pseudocode"},
	"func":      {"function", "procedure"},
	"xref":      {"xrefs", "references"},
	"attach":    {"frida", "instrument"},
	"apk":       {"android"},
	"exe":       {"pe", "windows", "executable"},
	"dll":       {"pe", "windows"},
	"golang":    {"go"},
	"dotnet":    {"managed", "dotnet"},
	"js":        {"javascript"},
	"macho":     {"macho"},
	"deps":      {"dependency"},
	"buildinfo": {"build", "version"},
}

func containsCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// expandTerms adds synonyms and Chinese-to-English mappings for each query
// token. Chinese phrases are matched by substring because a sentence often
// arrives as one whitespace-free token.
func expandTerms(tokens []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tokens))
	add := func(term string) {
		if len(term) < 2 || toolSearchStopWords[term] || seen[term] {
			return
		}
		seen[term] = true
		out = append(out, term)
	}
	for _, token := range tokens {
		add(token)
		for _, alias := range queryAliases[token] {
			add(alias)
		}
		if containsCJK(token) {
			for key, aliases := range queryAliases {
				if strings.Contains(token, key) {
					for _, alias := range aliases {
						add(alias)
					}
				}
			}
		}
	}
	return out
}

// hasTerm matches a whole token, with a prefix fallback for longer terms so
// "decompile" still finds "decompiler" while short tokens like "go" do not
// match "google".
func hasTerm(tokens []string, term string) bool {
	for _, token := range tokens {
		if token == term {
			return true
		}
		if len(term) >= 4 && strings.HasPrefix(token, term) {
			return true
		}
	}
	return false
}
