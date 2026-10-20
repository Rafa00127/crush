package agent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/permission"
)

//go:embed templates/agentic_fetch.md
var agenticFetchToolDescription string

// agenticFetchValidationResult holds the validated parameters from the tool call context.
type agenticFetchValidationResult struct {
	SessionID      string
	AgentMessageID string
}

// validateAgenticFetchParams validates the tool call parameters and extracts required context values.
func validateAgenticFetchParams(ctx context.Context, params tools.AgenticFetchParams) (agenticFetchValidationResult, error) {
	if params.Prompt == "" {
		return agenticFetchValidationResult{}, errors.New("prompt is required")
	}

	sessionID := tools.GetSessionFromContext(ctx)
	if sessionID == "" {
		return agenticFetchValidationResult{}, errors.New("session id missing from context")
	}

	agentMessageID := tools.GetMessageFromContext(ctx)
	if agentMessageID == "" {
		return agenticFetchValidationResult{}, errors.New("agent message id missing from context")
	}

	return agenticFetchValidationResult{
		SessionID:      sessionID,
		AgentMessageID: agentMessageID,
	}, nil
}

//go:embed templates/agentic_fetch_prompt.md.tpl
var agenticFetchPromptTmpl []byte

func (c *coordinator) agenticFetchTool(_ context.Context, client *http.Client) (fantasy.AgentTool, error) {
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
		tools.AgenticFetchToolName,
		agenticFetchToolDescription,
		func(ctx context.Context, params tools.AgenticFetchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			validationResult, err := validateAgenticFetchParams(ctx, params)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			// Determine description based on mode.
			var description string
			if params.URL != "" {
				description = fmt.Sprintf("Fetch and analyze content from URL: %s", params.URL)
			} else {
				description = "Search the web and analyze results"
			}

			p, err := c.permissions.Request(
				ctx,
				permission.CreatePermissionRequest{
					SessionID:   validationResult.SessionID,
					Path:        c.cfg.WorkingDir(),
					ToolCallID:  call.ID,
					ToolName:    tools.AgenticFetchToolName,
					Action:      "fetch",
					Description: description,
					Params:      tools.AgenticFetchPermissionsParams(params),
				},
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !p {
				return tools.NewPermissionDeniedResponse(), nil
			}

			tmpDir, err := os.MkdirTemp(c.cfg.Config().Options.DataDirectory, "crush-fetch-*")
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to create temporary directory: %s", err)), nil
			}
			defer os.RemoveAll(tmpDir)

			// Web search is the sub-agent's primary job, so choose the model
			// it runs on with search capability in mind. Providers speaking
			// the Anthropic Messages API can run web search server-side via
			// a provider-defined tool, which beats scraping DuckDuckGo
			// locally (faster, no rate limits, no proxy needed). When the
			// small model's provider cannot do that but the large model's
			// can, promote the sub-agent to the large model instead of
			// silently degrading to the local scraper.
			large, small, err := c.buildAgentModels(ctx, true)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error building models: %s", err)
			}

			subAgentModel := small
			subAgentProviderCfg, ok := c.cfg.Config().Providers.Get(small.ModelCfg.Provider)
			if !ok {
				return fantasy.ToolResponse{}, errors.New("small model provider not configured")
			}
			serverSideSearch := subAgentProviderCfg.Type == anthropic.Name
			if !serverSideSearch {
				if largeCfg, found := c.cfg.Config().Providers.Get(large.ModelCfg.Provider); found && largeCfg.Type == anthropic.Name {
					subAgentModel = large
					subAgentProviderCfg = largeCfg
					serverSideSearch = true
				}
			}

			var fullPrompt string

			if params.URL != "" {
				// URL mode: fetch the URL content first.
				content, err := tools.FetchURLAndConvert(ctx, client, params.URL)
				if err != nil {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to fetch URL: %s", err)), nil
				}

				hasLargeContent := len(content) > tools.LargeContentThreshold

				if hasLargeContent {
					tempFile, err := os.CreateTemp(tmpDir, "page-*.md")
					if err != nil {
						return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to create temporary file: %s", err)), nil
					}
					tempFilePath := tempFile.Name()

					if _, err := tempFile.WriteString(content); err != nil {
						tempFile.Close()
						return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to write content to file: %s", err)), nil
					}
					tempFile.Close()

					fullPrompt = fmt.Sprintf("%s\n\nThe web page from %s has been saved to: %s\n\nUse the view and grep tools to analyze this file and extract the requested information.", params.Prompt, params.URL, tempFilePath)
				} else {
					fullPrompt = fmt.Sprintf("%s\n\nWeb page URL: %s\n\n<webpage_content>\n%s\n</webpage_content>", params.Prompt, params.URL, content)
				}
			} else {
				// Search mode: let the sub-agent search and fetch as needed.
				searchHint := "Use the web_search tool to find relevant information. Break down the question into smaller, focused searches if needed. After searching, use web_fetch to get detailed content from the most relevant results."
				if serverSideSearch {
					// Server-side search runs inside the model turn, so
					// results arrive as sources rather than as a tool the
					// sub-agent has to drive itself.
					searchHint = "Use the web_search tool to find relevant information; the provider runs it and returns source titles and URLs. When a result lacks a usable excerpt, use web_fetch to read the page and extract details yourself. Break the question into focused searches if needed."
				}
				fullPrompt = fmt.Sprintf("%s\n\n%s", params.Prompt, searchHint)
			}

			promptOpts := []prompt.Option{
				prompt.WithWorkingDir(tmpDir),
				prompt.WithServerSideSearch(serverSideSearch),
			}

			promptTemplate, err := prompt.NewPrompt("agentic_fetch", string(agenticFetchPromptTmpl), promptOpts...)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error creating prompt: %s", err)
			}

			systemPrompt, err := promptTemplate.Build(ctx, subAgentModel.Model.Provider(), subAgentModel.Model.Model(), c.cfg)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error building system prompt: %s", err)
			}

			webFetchTool := tools.NewWebFetchTool(tmpDir, client)
			fetchTools := []fantasy.AgentTool{
				webFetchTool,
				tools.NewGlobTool(tmpDir, c.cfg.Config().Tools.Glob),
				tools.NewGrepTool(tmpDir, c.cfg.Config().Tools.Grep),
				tools.NewSourcegraphTool(client),
				tools.NewViewTool(c.lspManager, c.permissions, c.filetracker, nil, tmpDir),
			}

			// Server-side search replaces the local scraper outright when
			// available; running both would offer the model two tools with
			// the same name.
			var providerTools []fantasy.ProviderTool
			if serverSideSearch {
				providerTools = append(providerTools, tools.NewAnthropicWebSearchTool())
			} else {
				fetchTools = append(fetchTools, tools.NewWebSearchTool(client))
			}

			// Sub-agent tools run without hook interception. The top-level
			// `agentic_fetch` call itself is already wrapped from the coder's
			// side; firing hooks again for every inner tool call would run
			// the user's hooks N times per delegated turn.

			agent := NewSessionAgent(SessionAgentOptions{
				LargeModel:           subAgentModel, // Use one model for both (fetch doesn't need both)
				SmallModel:           subAgentModel,
				SystemPromptPrefix:   subAgentProviderCfg.SystemPromptPrefix,
				SystemPrompt:         systemPrompt,
				DisableAutoSummarize: c.cfg.Config().Options.DisableAutoSummarize,
				IsYolo:               c.permissions.SkipRequests(),
				Sessions:             c.sessions,
				Messages:             c.messages,
				Tools:                fetchTools,
				ProviderTools:        providerTools,
			})

			return c.runSubAgent(ctx, subAgentParams{
				Agent:          agent,
				SessionID:      validationResult.SessionID,
				AgentMessageID: validationResult.AgentMessageID,
				ToolCallID:     call.ID,
				Prompt:         fullPrompt,
				SessionTitle:   "Fetch Analysis",
				SessionSetup: func(sessionID string) {
					c.permissions.AutoApproveSession(sessionID)
				},
			})
		},
	), nil
}
