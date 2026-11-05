package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skhell/visio-to-drawio/internal/converter"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

type listInput struct {
	Path string `json:"path" jsonschema:"absolute or relative path to a .vsdx drawing or .vstx template"`
}

type listOutput struct {
	Pages []string `json:"pages"`
}

type convertInput struct {
	Path         string   `json:"path" jsonschema:"absolute or relative path to a .vsdx drawing or .vstx template"`
	OutputDir    string   `json:"output_dir,omitempty" jsonschema:"output folder (default: folder named after the input file, next to the current directory)"`
	Page         string   `json:"page,omitempty" jsonschema:"convert only this page (name, unique name or ID)"`
	Force        bool     `json:"force,omitempty" jsonschema:"overwrite existing output files"`
	NoFit        bool     `json:"no_fit,omitempty" jsonschema:"do not shrink text that would overflow its box"`
	SplitLayers  bool     `json:"split_layers,omitempty" jsonschema:"keep every visible Visio layer as its own draw.io layer"`
	HideLayers   []string `json:"hide_layers,omitempty" jsonschema:"Visio layer names to keep as separate, hidden draw.io layers"`
	BGEditable   bool     `json:"background_editable,omitempty" jsonschema:"leave background layers unlocked"`
	NoBackground bool     `json:"no_background,omitempty" jsonschema:"do not include BackPage background layers"`
}

type convertOutput struct {
	OutputDir string   `json:"output_dir"`
	Files     []string `json:"files"`
}

// checkVSDX validates the input path and, when VISIODRAWIO_ROOT is set,
// confines it (and the output folder) to that directory tree.
func checkVSDX(path string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".vsdx", ".vstx":
		return nil
	case ".vsd":
		return errLegacyVSD
	}
	return fmt.Errorf("%q is not a .vsdx or .vstx file", path)
}

func withinRoot(path string) error {
	root := os.Getenv("VISIODRAWIO_ROOT")
	if root == "" || path == "" {
		return nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q is outside VISIODRAWIO_ROOT", path)
	}
	return nil
}

func listPages(_ context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, listOutput, error) {
	if err := checkVSDX(in.Path); err != nil {
		return nil, listOutput{}, err
	}
	if err := withinRoot(in.Path); err != nil {
		return nil, listOutput{}, err
	}
	pkg, err := converter.Open(in.Path)
	if err != nil {
		return nil, listOutput{}, err
	}
	out := listOutput{Pages: []string{}}
	for _, pg := range pkg.ForegroundPages() {
		out.Pages = append(out.Pages, pg.Name())
	}
	return nil, out, nil
}

func convertFile(_ context.Context, _ *mcp.CallToolRequest, in convertInput) (*mcp.CallToolResult, convertOutput, error) {
	if err := checkVSDX(in.Path); err != nil {
		return nil, convertOutput{}, err
	}
	dir := in.OutputDir
	if dir == "" {
		dir = strings.TrimSuffix(filepath.Base(in.Path), filepath.Ext(in.Path))
	}
	for _, p := range []string{in.Path, dir} {
		if err := withinRoot(p); err != nil {
			return nil, convertOutput{}, err
		}
	}
	opt := converter.Options{NoFit: in.NoFit, Split: in.SplitLayers, NoBG: in.NoBackground, BGEditable: in.BGEditable, HideLayers: in.HideLayers}
	if err := run(in.Path, dir, in.Force, in.Page, false, opt); err != nil {
		return nil, convertOutput{}, err
	}
	out := convertOutput{OutputDir: dir, Files: []string{}}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out.Files = append(out.Files, p)
		}
		return nil
	})
	return nil, out, nil
}

// serveMCP runs a Model Context Protocol server over stdio.
// Nothing may be written to stdout except protocol messages;
// logs go to stderr.
func serveMCP(ctx context.Context) error {
	s := mcp.NewServer(&mcp.Implementation{Name: "visiodrawio", Version: version}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_pages",
		Description: "List the foreground pages of a Visio .vsdx drawing or .vstx template.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, listPages)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "convert",
		Description: "Convert a Visio .vsdx drawing or .vstx template to draw.io .drawio files (one combined file, one file per page, and a CSV report per page).",
	}, convertFile)
	return s.Run(ctx, &mcp.StdioTransport{})
}
