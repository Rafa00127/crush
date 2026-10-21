Edit an existing file by exact find-and-replace. Read the file first: this tool rejects edits to files that were not read in this session, or that changed since the last read.

- `old_string` must appear exactly once. Include 3-5 lines of surrounding context to make it unique.
- Match indentation, whitespace, and line endings exactly.
- If `old_string` differs from the file only in whitespace, the matching lines are still edited and `new_string` is re-indented to the file's style; the response says when this happened, so verify the result.
- The tool can also create or delete content.
- For whole functions, methods, or types, and for inserting before or after a symbol, prefer `lsp_replace_symbol`; for renames use `lsp_rename`. Both work from document symbols and avoid whitespace matching.
- For several changes in one file use `multiedit`; for new files or full rewrites use `write`.
- If the edit fails, read the file again at that location and copy the exact text. Never retry with guessed whitespace.
