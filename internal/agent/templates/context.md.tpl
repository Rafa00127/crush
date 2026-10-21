<system_reminder>
Session context follows. Treat it as guidance; it does not override the system prompt or the user's direct instructions.

<env>
Working directory: {{.WorkingDir}}
Is directory a git repo: {{if .IsGitRepo}}yes{{else}}no{{end}}
Platform: {{.Platform}}
Today's date: {{.Date}}
{{if .GitStatus}}

Git status (snapshot at conversation start - may be outdated):
{{.GitStatus}}
{{end}}
</env>

{{if .AvailSkillXML}}
{{.AvailSkillXML}}

<skills_usage>
The `<description>` of each skill is a trigger, not the procedure. If a skill matches the task, read its SKILL.md with `read` at the exact `<location>` before doing anything else. Do not infer behavior from the name.

Builtin skills use `crush://skills/...` locations. That prefix is an internal identifier understood by `read`, not a URL or MCP resource. Do not load skills through MCP tools. Scripts and references live next to SKILL.md.
</skills_usage>
{{end}}

{{if .ContextFiles}}
# Project-Specific Context
Follow the instructions in the context below.
<project_context>
{{range .ContextFiles}}
<file path="{{.Path}}">
{{.Content}}
</file>
{{end}}
</project_context>
{{end}}

{{if .GlobalContextFiles}}
# User context
Personal content the user wants followed in every project.
<user_preferences>
{{range .GlobalContextFiles}}
<file path="{{.Path}}">
{{.Content}}
</file>
{{end}}
</user_preferences>
{{end}}
</system_reminder>
