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
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/alecthomas/chroma/formatters/html"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/llir/llvm/asm"
	"github.com/llir/llvm/ir"
	"github.com/mewkiz/pkg/goutil"
	"github.com/mewkiz/pkg/jsonutil"
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
		if err := explore(llPath, m, dotDir, htmlDir, funcNames); err != nil {
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
func explore(llPath string, m *ir.Module, dotDir, htmlDir string, funcNames map[string]bool) error {
	// Get functions set by `-funcs` or all functions if `-funcs` not used.
	var funcs []*ir.Function
	for _, f := range m.Funcs {
		if len(funcNames) > 0 && !funcNames[f.Name()] {
			dbg.Printf("skipping function %q.", f.Name())
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
		if err := genVisualization(llPath, f, dotDir, htmlDir); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// genVisualization generates a visualization of the control flow analysis
// performed on the given function.
func genVisualization(llPath string, f *ir.Function, dotDir, htmlDir string) error {
	// Parse control flow primitives JSON file.
	funcName := f.Name()
	dbg.Printf("parsing primitives of function %q.", funcName)
	prims, err := parsePrims(dotDir, funcName)
	if err != nil {
		return errors.WithStack(err)
	}
	// Output visualization of control flow analysis in HTML format.
	for i, prim := range prims {
		step := i + 1
		nsteps := len(prims)
		htmlName := fmt.Sprintf("%s_%04d.html", funcName, step)
		htmlPath := filepath.Join(htmlDir, htmlName)
		htmlContent, err := genStep(llPath, f, prim, step, nsteps)
		if err != nil {
			return errors.WithStack(err)
		}
		dbg.Printf("creating file %q.", htmlPath)
		if err := ioutil.WriteFile(htmlPath, htmlContent, 0644); err != nil {
			return errors.WithStack(err)
		}
		if err := genLLVMHighlight(llPath, f, prim, step); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// genStep generates a visualization in HTML format of the intermediate step of
// the control flow analysis which recovered the control flow primitive of the
// given function.
func genStep(llPath string, f *ir.Function, prim *primitive.Primitive, step, nsteps int) ([]byte, error) {
	llName := pathutil.FileName(llPath)
	// TODO: embed step.tmpl in binary.
	srcDir, err := goutil.SrcDir("github.com/mewmew/explore/cmd/explore")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	tmplPath := filepath.Join(srcDir, "step.tmpl")
	ts, err := template.ParseFiles(tmplPath)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	t := ts.Lookup("step.tmpl")
	buf := &bytes.Buffer{}
	data := map[string]interface{}{
		"Step":     step,
		"NSteps":   nsteps,
		"FuncName": f.Name(),
		"LLName":   llName,
	}
	if err := t.Execute(buf, data); err != nil {
		return nil, errors.WithStack(err)
	}
	return buf.Bytes(), nil
}

// genLLVMHighlight generates a visualization in HTML format of the intermediate
// step of the control flow analysis, highlighting the lines of the LLVM IR for
// the corresponding basic blocks of the recovered high-level control flow
// primitive.
func genLLVMHighlight(llPath string, f *ir.Function, prim *primitive.Primitive, step int) error {
	// Get Chroma LLVM IR lexer.
	lexer := lexers.Get("llvm")
	if lexer == nil {
		lexer = lexers.Fallback
	}
	//lexer = chroma.Coalesce(lexer)
	// Get Chrome Monokai style.
	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}
	// Get Chroma HTML formatter.
	// Line number ranges to highlight; 1-based line numbers, inclusive.
	var highlightRanges [][2]int
	for _, blockName := range prim.Nodes {
		block, err := findBlock(f, blockName)
		if err != nil {
			return errors.WithStack(err)
		}
		highlightRange := lineRangeOfBlock(f, block)
		highlightRanges = append(highlightRanges, highlightRange)
	}
	formatter := html.New(
		html.TabWidth(3),
		html.WithLineNumbers(),
		// TODO: re-enable once https://github.com/alecthomas/chroma/issues/211 is fixed.
		//html.WithClasses(),
		html.LineNumbersInTable(),
		html.HighlightLines(highlightRanges),
	)

	// Write CSS.
	llvmContent := &bytes.Buffer{}
	llvmContent.WriteString("<!DOCTYPE html><html><head><style>")
	if err := formatter.WriteCSS(llvmContent, style); err != nil {
		return errors.WithStack(err)
	}
	iterator, err := lexer.Tokenise(nil, f.Def())
	if err != nil {
		return errors.WithStack(err)
	}
	llvmContent.WriteString("</style></head><body>")
	if err := formatter.Format(llvmContent, style, iterator); err != nil {
		return errors.WithStack(err)
	}
	llvmContent.WriteString("</body></html>")

	exploreDir := pathutil.TrimExt(llPath) + "_explore"
	llvmHTMLName := fmt.Sprintf("%s_llvm_%04d.html", f.Name(), step)
	llvmHTMLPath := filepath.Join(exploreDir, llvmHTMLName)
	dbg.Printf("creating %q", llvmHTMLPath)
	if err := ioutil.WriteFile(llvmHTMLPath, llvmContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// lineRangeOfBlock returns the line number range (1-based: [start, end]) of the
// basic block in the given function.
func lineRangeOfBlock(f *ir.Function, block *ir.BasicBlock) [2]int {
	funcStr := f.Def()
	blockStr := block.Def()
	pos := strings.Index(funcStr, blockStr)
	if pos == -1 {
		panic(fmt.Errorf("unable to locate contents of basic block %v in contents of function %v", block.Ident(), f.Ident()))
	}
	before := funcStr[:pos]
	start := 1 + strings.Count(before, "\n")
	n := strings.Count(blockStr, "\n")
	end := start + n
	return [2]int{start, end}
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
// For a source file "foo.ll" the output directory "foo_explore/" is created. If
// the `-force` flag is set, existing graph directories are overwritten by
// force.
func createHTMLDir(llPath string, force bool) (string, error) {
	var dotDir string
	switch llPath {
	case "-":
		dotDir = "stdin_explore"
	default:
		dotDir = pathutil.TrimExt(llPath) + "_explore"
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

// parsePrims parses the recovered control flow primitives of the given
// function.
func parsePrims(dotDir, funcName string) ([]*primitive.Primitive, error) {
	jsonName := funcName + ".json"
	jsonPath := filepath.Join(dotDir, jsonName)
	var prims []*primitive.Primitive
	if err := jsonutil.ParseFile(jsonPath, &prims); err != nil {
		return nil, errors.WithStack(err)
	}
	return prims, nil
}

// findBlock locates and returns the basic block with the specified name in the
// given function.
func findBlock(f *ir.Function, blockName string) (*ir.BasicBlock, error) {
	for _, block := range f.Blocks {
		if block.Name() == blockName {
			return block, nil
		}
	}
	return nil, errors.Errorf("unable to locate basic block %q in function %q", blockName, f.Name())
}
