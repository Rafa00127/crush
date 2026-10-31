Fetch raw content from a URL as text, markdown, or html (max {{ .MaxFetchSizeKB }}KB); no AI processing. For a single page's analysis or extraction use agentic_fetch, which keeps the raw text out of your context; for several pages of research it is the right tool.
{{- if .GhAvailable }} For GitHub content when an exact repo, issue, or PR link is provided, use `gh` CLI in bash instead.{{- end }}
