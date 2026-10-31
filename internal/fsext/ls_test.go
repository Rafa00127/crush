package fsext

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListDirectory(t *testing.T) {
	tmp := t.TempDir()

	testFiles := map[string]string{
		"regular.txt":     "content",
		".hidden":         "hidden content",
		".gitignore":      ".*\n*.log\n",
		"subdir/file.go":  "package main",
		"subdir/.another": "more hidden",
		"build.log":       "build output",
	}

	for name, content := range testFiles {
		fp := filepath.Join(tmp, name)
		dir := filepath.Dir(fp)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(fp, []byte(content), 0o644))
	}

	t.Run("no limit", func(t *testing.T) {
		files, truncated, err := ListDirectory(tmp, nil, -1, -1)
		require.NoError(t, err)
		require.False(t, truncated)
		// The .gitignore has ".*" pattern which ignores hidden files anywhere
		// (like real git does), so subdir/.another is ignored.
		require.Len(t, files, 3)
		require.ElementsMatch(t, []string{
			"regular.txt",
			"subdir",
			"subdir/file.go",
		}, relPaths(t, files, tmp))
	})
	t.Run("limit", func(t *testing.T) {
		files, truncated, err := ListDirectory(tmp, nil, -1, 2)
		require.NoError(t, err)
		require.True(t, truncated)
		require.Len(t, files, 2)
	})
	t.Run("binaries survive gitignore", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.exe\n*.dll\n"), 0o644))
		for _, name := range []string{"app.exe", "lib.dll", "main.go"} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644))
		}

		files, _, err := ListDirectory(dir, nil, -1, -1)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{
			".gitignore",
			"app.exe",
			"lib.dll",
			"main.go",
		}, relPaths(t, files, dir))
	})
	t.Run("explicit ignore hides binaries", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "app.exe"), []byte("x"), 0o644))

		files, _, err := ListDirectory(dir, []string{"*.exe"}, -1, -1)
		require.NoError(t, err)
		require.Empty(t, files)
	})
}

func relPaths(tb testing.TB, in []string, base string) []string {
	tb.Helper()
	out := make([]string, 0, len(in))
	for _, p := range in {
		rel, err := filepath.Rel(base, p)
		require.NoError(tb, err)
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}
