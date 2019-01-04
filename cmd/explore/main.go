// The explore tool visualizes the stages of the decompiler pipeline (*.ll ->
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
//   -f    force overwrite existing explore directories
//   -funcs string
//         comma-separated list of functions to parse
//   -q    suppress non-error messages
//   -style string
//         style used for syntax highlighting (borland, monokai, vs, ...)
//         (default "vs")
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/llir/llvm/ir"
	"github.com/mewkiz/pkg/osutil"
	"github.com/mewkiz/pkg/pathutil"
	"github.com/mewkiz/pkg/term"
	"github.com/mewmew/lnp/pkg/cfa/primitive"
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
Visualize the stages of the decompiler pipeline.

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
		// style specifies the style used for syntax highlighting.
		style string
	)
	flag.BoolVar(&force, "f", false, "force overwrite existing explore directories")
	flag.StringVar(&funcs, "funcs", "", "comma-separated list of functions to parse")
	flag.BoolVar(&quiet, "q", false, "suppress non-error messages")
	flag.StringVar(&style, "style", "vs", "style used for syntax highlighting (borland, monokai, vs, ...)")
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
		// Parse LLVM IR module.
		e := newExplorer(llPath, style)
		m, err := parseModule(llPath)
		if err != nil {
			log.Fatalf("%+v", err)
		}
		e.m = m
		if len(m.Funcs) == 0 {
			warn.Printf("no functions in module %q", llPath)
			continue
		}
		// Parse debug LLVM IR module if present.
		llDbgPath := pathutil.TrimExt(llPath) + "_dbg.ll"
		if osutil.Exists(llDbgPath) {
			dbg, err := parseModule(llDbgPath)
			if err != nil {
				log.Fatalf("%+v", err)
			}
			e.dbg = dbg
		}
		// Generate HTML visualizations.
		if err := e.explore(funcNames, force); err != nil {
			log.Fatalf("%+v", err)
		}
	}
}

// explore generates an HTML visualization of the control flow analysis
// performed on each function of the given LLVM IR module.
//
// - funcNames specifies the set of function names for which to generate
//   visualizations. When funcNames is emtpy, visualizations are generated for
//   all function definitions of the module.
//
// - force specifies whether to force overwrite existing explore directories.
func (e *explorer) explore(funcNames map[string]bool, force bool) error {
	// Get functions set by `-funcs` or all functions if `-funcs` not used.
	var funcs []*ir.Func
	for _, f := range e.m.Funcs {
		if len(funcNames) > 0 && !funcNames[f.Name()] {
			dbg.Printf("skipping function %q", f.Name())
			continue
		}
		funcs = append(funcs, f)
	}
	// Initialize visualization, create output directory, parse template assets,
	// and copy styles.
	if err := e.init(force); err != nil {
		return errors.WithStack(err)
	}
	// Generate control flow graphs in DOT format.
	if err := e.outputCFGs(funcNames); err != nil {
		return errors.WithStack(err)
	}
	// Generate a visualization of the control flow analysis performed on each
	// function.
	for _, f := range funcs {
		// Skip function declarations.
		if len(f.Blocks) == 0 {
			continue
		}
		// Generate visualization for the given function.
		if err := e.outputFuncVisualization(f); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// outputFuncVisualization outputs a visualization of the control flow analysis
// performed on the given function.
//
// - f is the function to visualize.
func (e *explorer) outputFuncVisualization(f *ir.Func) error {
	// Generate control flow primtives in JSON format.
	funcName := f.Name()
	if err := e.outputPrims(funcName); err != nil {
		return errors.WithStack(err)
	}
	// Parse control flow primitives JSON file.
	dbg.Printf("parsing primitives of function %q", funcName)
	prims, err := e.parsePrims(funcName)
	if err != nil {
		return errors.WithStack(err)
	}
	// Parse original C source code.
	cSource, err := e.parseC()
	if err != nil {
		return errors.WithStack(err)
	}
	hasC := len(cSource) > 0
	npages := 1 + 2*len(prims)
	for page := 1; page <= npages; page++ {
		// Output overview.
		//
		//    page 1: step 0
		//    page 2: step 1a
		//    page 3: step 1b
		//    page 4: step 2a
		//    page 5: step 2b
		//    ...
		step := page / 2
		subStep := subStepFromPage(page)
		if err := e.outputOverview(funcName, page, npages, step, subStep); err != nil {
			return errors.WithStack(err)
		}
		// Output control flow analysis.
		if err := e.outputCFA(funcName, step, subStep); err != nil {
			return errors.WithStack(err)
		}
		// Output reconstructed Go source code.
		if err := e.outputGo(funcName, prims, step, subStep); err != nil {
			return errors.WithStack(err)
		}
	}
	nsteps := len(prims)
	for step := 0; step <= nsteps; step++ {
		// Output original C source code.
		var prim *primitive.Primitive
		if step > 0 {
			// Visualize control flow analysis of recovered control flow primitive,
			// except for on step 0.
			prim = prims[step-1]
		}
		if hasC {
			if err := e.outputC(cSource, funcName, prim, step); err != nil {
				return errors.WithStack(err)
			}
		}
		// Output LLVM IR assembly.
		if err := e.outputLLVM(funcName, prim, step); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// outputCFGs outputs the control flow graphs of the given LLVM IR module by
// running the ll2dot tool.
//
// - funcNames specifies the set of function names for which to generate
//   visualizations. When funcNames is emtpy, visualizations are generated for
//   all function definitions of the module.
func (e *explorer) outputCFGs(funcNames map[string]bool) error {
	var args []string
	if len(funcNames) > 0 {
		var funcs []string
		for funcName := range funcNames {
			funcs = append(funcs, funcName)
		}
		sort.Strings(funcs)
		args = append(args, "-funcs", strings.Join(funcs, ","))
	}
	args = append(args, "-f", "-img", e.llPath)
	cmd := exec.Command("ll2dot2", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// outputPrims outputs the recovered control flow primitives of the given LLVM
// IR module by running the restructure tool.
func (e *explorer) outputPrims(funcName string) error {
	jsonName := funcName + ".json"
	jsonPath := filepath.Join(e.dotDir, jsonName)
	dotName := funcName + ".dot"
	dotPath := filepath.Join(e.dotDir, dotName)
	cmd := exec.Command("restructure2", "-steps", "-img", "-indent", "-o", jsonPath, dotPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.WithStack(err)
	}
	return nil
}
