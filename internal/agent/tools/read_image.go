package tools

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/filepathext"
	"github.com/charmbracelet/crush/internal/permission"
)

// ReadImageToolName is the tool that hands a picture to the model.
const ReadImageToolName = "read_image"

//go:embed read_image.md
var readImageDescription string

// ReadImageParams is the tool input.
type ReadImageParams struct {
	Path string `json:"path" description:"Path to a PNG, JPEG, WebP or GIF file"`
}

// NewReadImageTool creates the image-only reader used where a full file tool
// would cost more schema than it is worth. The read tool already handles
// images; this one exists so a minimal toolset can still look at a picture
// without carrying the whole file reader.
func NewReadImageTool(permissions permission.Service, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ReadImageToolName,
		readImageDescription,
		func(ctx context.Context, params ReadImageParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Path == "" {
				return fantasy.NewTextErrorResponse("missing path"), nil
			}
			filePath := filepathext.SmartJoin(workingDir, params.Path)

			if !withinWorkingDir(workingDir, filePath) {
				sessionID := GetSessionFromContext(ctx)
				if sessionID == "" {
					return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for accessing files outside working directory")
				}
				granted, permErr := permissions.Request(
					ctx,
					permission.CreatePermissionRequest{
						SessionID:   sessionID,
						Path:        filePath,
						ToolCallID:  call.ID,
						ToolName:    ReadImageToolName,
						Action:      "read",
						Description: fmt.Sprintf("Read image outside working directory: %s", filePath),
					},
				)
				if permErr != nil {
					return fantasy.ToolResponse{}, permErr
				}
				if !granted {
					return NewPermissionDeniedResponse(), nil
				}
			}

			fileInfo, err := os.Stat(filePath)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to read image: %s", err)), nil
			}
			if fileInfo.IsDir() {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("%s: is a directory", filePath)), nil
			}
			ok, mimeType := getImageMimeType(filePath)
			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("%s: not a supported image type (PNG, JPEG, WebP, GIF)", filePath)), nil
			}
			if fileInfo.Size() > MaxImageSize {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Image file is too large (%d bytes). Maximum size is %d bytes", fileInfo.Size(), MaxImageSize)), nil
			}
			if !GetSupportsImagesFromContext(ctx) {
				return UnsupportedMediaResponse(GetModelNameFromContext(ctx)), nil
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to read image file: %s", err)), nil
			}
			// The sniffed type wins: some tools write JPEG bytes into a .png
			// name, and providers validate the declared type against the
			// bytes.
			return fantasy.NewImageResponse(data, sniffImageMimeType(data, mimeType)), nil
		},
	)
}

// withinWorkingDir reports whether path resolves inside dir.
func withinWorkingDir(dir, path string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, absPath)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
