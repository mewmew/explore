package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/alecthomas/chroma/formatters/html"
	"github.com/alecthomas/chroma/styles"
	"github.com/llir/llvm/ir"
	"github.com/mewkiz/pkg/goutil"
	"github.com/mewkiz/pkg/pathutil"
	dircopy "github.com/otiai10/copy"
	"github.com/pkg/errors"
)

// explorer configures the output environment of the visualization.
type explorer struct {
	// LLVM IR assembly path.
	llPath string
	// LLVM IR module (foo.ll).
	m *ir.Module
	// Debug LLVM IR module (foo_dbg.ll); or nil if not present.
	dbg *ir.Module
	// Base name (name of LLVM IR assembly file without extension).
	base string
	// Explore output directory.
	outputDir string
	// Control flow graph directory.
	dotDir string
	// Chroma style name used for syntax highlighting.
	style string
	// Explore GitHub repository directory, from which HTML template assets are
	// located.
	repoDir string
	// Template for overview HTML page.
	overviewTmpl *template.Template
	// Template for C HTML page.
	cTmpl *template.Template
	// Template for LLVM HTML page.
	llvmTmpl *template.Template
	// Template for the control flow analysis HTML page.
	cfaTmpl *template.Template
	// Template for Go HTML page.
	goTmpl *template.Template
}

// newExplorer returns a new explorer which configures the output environment of
// the visualization.
func newExplorer(llPath, style string) *explorer {
	var base string
	switch llPath {
	case "-":
		base = "stdin"
	default:
		base = pathutil.TrimExt(llPath)
	}
	return &explorer{
		llPath:    llPath,
		base:      base,
		outputDir: base + "_explore",
		dotDir:    base + "_graphs",
		style:     style,
	}
}

// init initializes the visualization, creates the output directory, parses
// template assets, and copies CSS stylesheets.
//
// - force specifies whether to force overwrite existing explore directories.
func (e *explorer) init(force bool) error {
	// Create HTML visualization output directory.
	if err := e.createOutputDir(force); err != nil {
		return errors.WithStack(err)
	}
	// Locate Explore GitHub repository directory.
	if err := e.findRepoDir(); err != nil {
		return errors.WithStack(err)
	}
	// Parse HTML templates of visualization.
	if err := e.parseTemplates(); err != nil {
		return errors.WithStack(err)
	}
	// Copy CSS include files.
	if err := e.copyStyles(); err != nil {
		return errors.WithStack(err)
	}
	// Output Chroma CSS stylesheet.
	if err := e.outputChromaStyle(); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// createOutputDir creates the visualization output directory based on the path
// of the LLVM IR assembly file.
//
// For a source file "foo.ll" the output directory "foo_explore/" is created. If
// the `-force` flag is set, existing explore directories are overwritten by
// force.
func (e *explorer) createOutputDir(force bool) error {
	if force {
		// Force overwrite existing graph directories.
		if err := os.RemoveAll(e.outputDir); err != nil {
			return errors.WithStack(err)
		}
	}
	if err := os.Mkdir(e.outputDir, 0755); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// findRepoDir locates the Explore GitHub repository directory.
func (e *explorer) findRepoDir() error {
	repoDir, err := goutil.SrcDir("github.com/mewmew/explore")
	if err != nil {
		return errors.WithStack(err)
	}
	e.repoDir = repoDir
	return nil
}

// parseTemplates parses the HTML templates of the visualization.
func (e *explorer) parseTemplates() error {
	if err := e.parseOverviewTemplate(); err != nil {
		return errors.WithStack(err)
	}
	if err := e.parseCTemplate(); err != nil {
		return errors.WithStack(err)
	}
	if err := e.parseLLVMTemplate(); err != nil {
		return errors.WithStack(err)
	}
	if err := e.parseCFATemplate(); err != nil {
		return errors.WithStack(err)
	}
	return e.parseGoTemplate()
}

// copyStyles copies the styles to the explore output directory.
func (e *explorer) copyStyles() error {
	// Locate CSS files.
	srcPath := filepath.Join(e.repoDir, "inc")
	// Copy CSS files.
	dstPath := filepath.Join(e.outputDir, "inc")
	dbg.Printf("creating %q", dstPath)
	if err := dircopy.Copy(srcPath, dstPath); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// outputChromaStyle outputs the Chroma CSS stylesheet to the inc/css
// subdirectory of the visualization output directory.
func (e *explorer) outputChromaStyle() error {
	// Get Chrome style.
	style := styles.Get(e.style)
	if style == nil {
		style = styles.Fallback
	}
	// Get Chroma HTML formatter.
	formatter := html.New(
		html.TabWidth(3),
		html.WithLineNumbers(),
		html.WithClasses(),
		html.LineNumbersInTable(),
	)
	// Output CSS Chroma stylesheet.
	cssContent := &bytes.Buffer{}
	if err := formatter.WriteCSS(cssContent, style); err != nil {
		return errors.WithStack(err)
	}
	cssName := filepath.Base(fmt.Sprintf("chroma_%s.css", e.style))
	cssPath := filepath.Join(e.outputDir, "inc/css", cssName)
	dbg.Printf("creating %q", cssPath)
	if err := ioutil.WriteFile(cssPath, cssContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}
