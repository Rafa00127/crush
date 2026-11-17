Run commands in a bash shell.
- Every call is a new shell: chain steps that must share state with `&&`; `cd` and exported variables do not carry over.
- On Windows this is msys bash: `C:` is `/c`, `D:` is `/d`.
- Inspect a range with `sed -n '10,25p' file` rather than printing the whole file, and avoid commands with very large output.
- For a long command set run_in_background and keep working: its output arrives when it finishes.
- For the web use the fetch and web_search tools, not curl or wget: those are blocked as dangerous commands.
- Dangerous commands (privilege escalation, package managers, curl/wget, ssh, system changes) are blocked or need approval. Command substitution is **not** filtered when commands run through a real bash, so treat `$(…)` with the same caution. Find another way or ask.
