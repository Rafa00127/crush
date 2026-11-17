package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// onePixelPNG is a valid 1x1 PNG, enough for the mime sniffing and the read.
var onePixelPNG = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
	0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
	0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

func readImageToolForTest(t *testing.T, dir string) fantasy.AgentTool {
	t.Helper()
	perms := &mockBashPermissionService{Broker: pubsub.NewBroker[permission.PermissionRequest]()}
	return NewReadImageTool(perms, dir)
}

// A picture inside the working directory comes back as an image.
func TestReadImageToolReturnsImage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	require.NoError(t, os.WriteFile(path, onePixelPNG, 0o644))

	tool := readImageToolForTest(t, dir)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "test-session")
	ctx = context.WithValue(ctx, SupportsImagesContextKey, true)

	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "c1", Name: ReadImageToolName, Input: `{"path":"shot.png"}`})
	require.NoError(t, err)
	require.Equal(t, "image", resp.Type, "an image file must come back as media: %+v", resp)
	require.Equal(t, "image/png", resp.MediaType)
	require.NotEmpty(t, resp.Data)
}

// A model without image support is told so instead of receiving bytes it
// cannot use.
func TestReadImageToolRefusesWithoutImageSupport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shot.png"), onePixelPNG, 0o644))

	tool := readImageToolForTest(t, dir)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "test-session")

	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "c2", Name: ReadImageToolName, Input: `{"path":"shot.png"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "does not support image")
}

// Anything that is not an image is refused with the reason, so the model can
// fall back to the shell instead of retrying variations.
func TestReadImageToolRefusesNonImage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644))

	tool := readImageToolForTest(t, dir)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "test-session")
	ctx = context.WithValue(ctx, SupportsImagesContextKey, true)

	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "c3", Name: ReadImageToolName, Input: `{"path":"notes.txt"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "not a supported image type")
}
