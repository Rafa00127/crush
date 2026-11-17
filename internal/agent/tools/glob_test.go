package tools

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGlobFilesScopedPrefixMatchesUnscoped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mkfile := func(rel string) {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte("x"), 0o644))
	}
	mkfile("a/b/one.go")
	mkfile("a/b/c/two.go")
	mkfile("a/other.txt")
	mkfile("z/three.go")

	// Paths are compared in the slash form glob reports, never with the
	// platform separator: on Windows filepath.Join gives a\b for a path glob
	// returns as a/b, which is what broke this test.
	scoped, _, err := globFiles(context.Background(), "a/**/*.go", root, 100)
	require.NoError(t, err)
	require.Len(t, scoped, 2)

	unscoped, _, err := globFiles(context.Background(), "**/*.go", root, 100)
	require.NoError(t, err)

	// Scoping the walk to the pattern's literal prefix is an optimisation: the
	// scoped call must return exactly the unscoped matches that live under the
	// prefix, no more (z/three.go stays out) and no fewer.
	want := make([]string, 0, len(unscoped))
	prefix := filepath.ToSlash(filepath.Join(root, "a")) + "/"
	for _, p := range unscoped {
		if strings.HasPrefix(filepath.ToSlash(p), prefix) {
			want = append(want, p)
		}
	}
	require.Len(t, want, 2, "the fixture has two .go files under a/")
	require.Equal(t, slashedSorted(want), slashedSorted(scoped))
}

// slashedSorted normalises paths for comparison: glob reports forward slashes
// on every platform, but the backend that produced them does not always.
func slashedSorted(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, filepath.ToSlash(p))
	}
	slices.Sort(out)
	return out
}

func TestGlobFilesDoesNotFollowSymlinkEscape(t *testing.T) {
	t.Parallel()

	// Build a project dir with a symlink pointing outside it. With symlink
	// following disabled, the glob must not pick up files behind the link.
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.go"), []byte("x"), 0o644))

	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "in.go"), []byte("x"), 0o644))
	link := filepath.Join(project, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	got, _, err := globFiles(context.Background(), "**/*.go", project, 100)
	require.NoError(t, err)
	for _, p := range got {
		require.NotContains(t, p, "secret.go", "glob followed a symlink out of the search root")
	}
}

func TestGlobFilesCapsResultsOnLargeTree(t *testing.T) {
	t.Parallel()

	// A tree with far more matches than the limit must still return at
	// most `limit` results and report truncation, regardless of which
	// backend (ripgrep or the doublestar fallback) runs.
	root := t.TempDir()
	for i := range 500 {
		dir := filepath.Join(root, "pkg")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		name := filepath.Join(dir, "file"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+"_"+string(rune('0'+i%10))+".go")
		require.NoError(t, os.WriteFile(name, []byte("x"), 0o644))
	}

	got, truncated, err := globFiles(context.Background(), "**/*.go", root, 10)
	require.NoError(t, err)
	require.LessOrEqual(t, len(got), 10, "must not exceed limit")
	require.True(t, truncated, "should report truncation on an over-limit tree")
}

// The filesystem walker reports directories when the pattern matches them; the
// tool must hand back files only, or the model gets "subdir" mixed into a list
// of paths it is about to open.
func TestKeepFilesDropsDirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	file := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(file, []byte("package a\n"), 0o644))

	got := keepFiles([]string{dir, filepath.Join(dir, "sub"), file})
	require.Equal(t, []string{file}, got)
}
