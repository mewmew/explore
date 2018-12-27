// The explore tool visualizes the stages of a decompiler pipeline (*.ll ->
// *.html).
//
// The input of explore is LLVM IR assembly and the output is a set of HTML
// files, each representing a visualization of the control flow analysis of a
// function.
//
// For a source file "foo.ll" containing the functions "bar" and "baz" the
// following HTML files are generated.
//
//    * foo_explore/bar.html
//    * foo_explore/baz.html
//
// Usage:
//
//     explore [OPTION]... [FILE.ll]...
//
// Flags:
//
//   -f    force overwrite existing graph directories
//   -funcs string
//         comma-separated list of functions to parse
//   -q    suppress non-error messages
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/llir/llvm/asm"
	"github.com/llir/llvm/ir"
	"github.com/mewkiz/pkg/pathutil"
	"github.com/mewkiz/pkg/term"
	"github.com/pkg/errors"
)

var (
	// dbg represents a logger with the "explore:" prefix, which logs debug
	// messages to standard error.
	dbg = log.New(os.Stderr, term.YellowBold("explore:")+" ", 0)
	// warn represents a logger with the "explore:" prefix, which logs warning
	// messages to standard error.
	warn = log.New(os.Stderr, term.RedBold("explore:")+" ", 0)
)

func usage() {
	const use = `
Visualize the stages of a decompiler pipeline.

Usage:

	explore [OPTION]... [FILE.ll]

Flags:
`
	fmt.Fprintln(os.Stderr, use[1:])
	flag.PrintDefaults()
}

func main() {
	// Parse command line arguments.
	var (
		// force specifies whether to force overwrite existing explore
		// directories.
		force bool
		// funcs represents a comma-separated list of functions to parse.
		funcs string
		// quiet specifies whether to suppress non-error messages.
		quiet bool
	)
	flag.BoolVar(&force, "f", false, "force overwrite existing explore directories")
	flag.StringVar(&funcs, "funcs", "", "comma-separated list of functions to parse")
	flag.BoolVar(&quiet, "q", false, "suppress non-error messages")
	flag.Usage = usage
	flag.Parse()
	var llPaths []string
	switch flag.NArg() {
	case 0:
		// Parse LLVM IR module from standard input.
		llPaths = []string{"-"}
	default:
		llPaths = flag.Args()
	}
	// Parse functions specified by the `-funcs` flag.
	funcNames := make(map[string]bool)
	for _, funcName := range strings.Split(funcs, ",") {
		funcName = strings.TrimSpace(funcName)
		if len(funcName) == 0 {
			continue
		}
		funcNames[funcName] = true
	}
	if quiet {
		// Mute debug messages if `-q` is set.
		dbg.SetOutput(ioutil.Discard)
	}

	// Generation visualization.
	for _, llPath := range llPaths {
		// Parse LLMV IR assembly file.
		m, err := parseModule(llPath)
		if err != nil {
			log.Fatalf("%+v", err)
		}
		if len(m.Funcs) == 0 {
			warn.Printf("no functions in module %q", llPath)
			continue
		}
		// Get DOT graphs directory.
		dotDir, err := getDOTDir(llPath)
		if err != nil {
			log.Fatalf("%+v", err)
		}
		// Create HTML visualization output directory.
		htmlDir, err := createHTMLDir(llPath, force)
		if err != nil {
			log.Fatalf("%+v", err)
		}
		// Generate HTML visualizations.
		if err := explore(m, dotDir, htmlDir, funcNames); err != nil {
			log.Fatalf("%+v", err)
		}
	}
}

// explore generates an HTML visualization of the control flow analysis
// performed on each function of the given LLVM IR module.
//
// dotDir specifies the directory of the control flow graphs and control flow
// analysis results of each function.
//
// funcNames specifies the set of function names for which to generate
// visualizations. When funcNames is emtpy, visualizations are generated for all
// function definitions of the module.
func explore(m *ir.Module, dotDir, htmlDir string, funcNames map[string]bool) error {
	// Get functions set by `-funcs` or all functions if `-funcs` not used.
	var funcs []*ir.Function
	for _, f := range m.Funcs {
		if len(funcNames) > 0 && !funcNames[f.Name()] {
			dbg.Printf("skipping function %q.", f.Ident())
			continue
		}
		funcs = append(funcs, f)
	}
	// Generate a visualization of the control flow analysis performed on each
	// function.
	for _, f := range funcs {
		// Skip function declarations.
		if len(f.Blocks) == 0 {
			continue
		}
		// Generate visualization.
		dbg.Printf("parsing function %q.", f.Ident())
		var htmlContent []byte
		// TODO: generate visualization.
		// Output visualization of control flow analysis in HTML format.
		if err := outputHTML(htmlContent, f.Name(), htmlDir); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// getDOTDir returns the control flow graphs directory based on the path of the
// LLVM IR file.
//
// For a source file "foo.ll" the output directory "foo_graphs" is returned.
func getDOTDir(llPath string) (string, error) {
	var dotDir string
	switch llPath {
	case "-":
		dotDir = "stdin_graphs"
	default:
		dotDir = pathutil.TrimExt(llPath) + "_graphs"
	}
	return dotDir, nil
}

// createHTMLDir creates and returns an HTML visualization output directory
// based on the path of the LLVM IR file.
//
// For a source file "foo.ll" the output directory "foo_graphs/" is created. If
// the `-force` flag is set, existing graph directories are overwritten by
// force.
func createHTMLDir(llPath string, force bool) (string, error) {
	var dotDir string
	switch llPath {
	case "-":
		dotDir = "stdin_graphs"
	default:
		dotDir = pathutil.TrimExt(llPath) + "_graphs"
	}
	if force {
		// Force overwrite existing graph directories.
		if err := os.RemoveAll(dotDir); err != nil {
			return "", errors.WithStack(err)
		}
	}
	if err := os.Mkdir(dotDir, 0755); err != nil {
		return "", errors.WithStack(err)
	}
	return dotDir, nil
}

// parseModule parses the given LLVM IR assembly file into an LLVM IR module.
func parseModule(llPath string) (*ir.Module, error) {
	switch llPath {
	case "-":
		// Parse LLVM IR module from standard input.
		dbg.Printf("parsing standard input.")
		return asm.Parse("stdin", os.Stdin)
	default:
		dbg.Printf("parsing file %q.", llPath)
		return asm.ParseFile(llPath)
	}
}

// outputHTML outputs the given control flow analysis visualization in HTML
// format.
//
// For a source file "foo.ll" containing the functions "bar" and "baz" the
// following HTML files are produced:
//
//    foo_explore/bar.html
//    foo_explore/baz.html
func outputHTML(htmlContent []byte, funcName, htmlDir string) error {
	htmlName := funcName + ".dot"
	htmlPath := filepath.Join(htmlDir, htmlName)
	dbg.Printf("creating file %q.", htmlPath)
	if err := ioutil.WriteFile(htmlPath, htmlContent, 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}
