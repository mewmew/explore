package main

import "github.com/mewkiz/pkg/pathutil"

// Explorer configures the output environment of the visualization.
type Explorer struct {
	// LLVM IR assembly path.
	LLPath string
	// Explore output directory.
	OutputDir string
	// Chroma style used for syntax highlighting.
	Style string
}

// NewExplorer returns a new explorer which configures the output environment of
// the visualization.
func NewExplorer(llPath, style string) *Explorer {
	var outputDir string
	switch llPath {
	case "-":
		outputDir = "stdin_explore"
	default:
		outputDir = pathutil.TrimExt(llPath) + "_explore"
	}
	return &Explorer{
		LLPath:    llPath,
		OutputDir: outputDir,
		Style:     style,
	}
}
