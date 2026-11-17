Run commands in a bash shell.
- Every call is a new shell: chain steps that must share state with `&&`; `cd` and exported variables do not carry over.
- On Windows this is msys bash: `C:` is `/c`, `D:` is `/d`.
- Inspect a range with `sed -n '10,25p' file` rather than printing the whole file, and avoid commands with very large output.
- For a long command set run_in_background and keep working: its output arrives when it finishes.
- Dangerous commands (privilege escalation, package managers, curl/wget, ssh, system changes, any command with a substitution in its arguments) are blocked or need approval. Find another way or ask.
