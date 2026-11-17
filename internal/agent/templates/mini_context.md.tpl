<system-reminder>
The following information describes your current session:

Current working directory: "{{.WorkingDir}}"{{if .IsGitRepo}}{{with .GitBranch}}, git branch {{.}}{{end}}{{if .GitDirty}} (uncommitted changes){{end}}{{else}}, not a git repository{{end}}.
</system-reminder>
