<system-reminder>
The following information describes your current session:

Current working directory: "{{.WorkingDir}}"{{if .IsGitRepo}}{{with .GitBranch}}, git branch {{.}}{{end}}{{if .GitDirty}} (uncommitted changes){{end}}{{else}}, not a git repository{{end}}.
{{- if .AvailSkillXML}}

{{.AvailSkillXML}}
<skills_usage>
The `<description>` of each skill is a trigger, not the procedure. If a skill matches the task, read its SKILL.md with `read` at the exact `<location>` before acting; the name alone does not describe the steps. Builtin skills use `crush://skills/...` locations, which `read` understands.
</skills_usage>
{{- end}}
{{- if .ContextFiles}}

<context_files>
Project context files, listed rather than loaded: read the one you need with `read`.
{{range .ContextFiles}}- {{.Path}}
{{end}}</context_files>
{{- end}}
{{- if .GlobalContextFiles}}

<global_context_files>
User-wide context files, listed rather than loaded: read one with `read` when it applies.
{{range .GlobalContextFiles}}- {{.Path}}
{{end}}</global_context_files>
{{- end}}
</system-reminder>
