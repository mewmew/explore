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
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	dircopy "github.com/otiai10/copy"
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
		e := NewExplorer(llPath, style)
		htmlDir, err := e.createHTMLDir(force)
		if err != nil {
			log.Fatalf("%+v", err)
		}
		// Generate HTML visualizations.
		if err := e.explore(m, dotDir, htmlDir, funcNames); err != nil {
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
func (e *Explorer) explore(m *ir.Module, dotDir, htmlDir string, funcNames map[string]bool) error {
	// Get functions set by `-funcs` or all functions if `-funcs` not used.
	var funcs []*ir.Func
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
		if err := e.genVisualization(m, f, dotDir, htmlDir); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// genVisualization generates a visualization of the control flow analysis
// performed on the given function.
func (e *Explorer) genVisualization(m *ir.Module, f *ir.Func, dotDir, htmlDir string) error {
	// Parse control flow primitives JSON file.
	funcName := f.Name()
	dbg.Printf("parsing primitives of function %q.", funcName)
	prims, err := parsePrims(dotDir, funcName)
	if err != nil {
		return errors.WithStack(err)
	}

	// Copy CSS include files.
	if err := e.copyStyles(); err != nil {
		return errors.WithStack(err)
	}

	// First overview.
	nsteps := 1 + 2*len(prims)
	if err := e.highlightGo(f.Name(), 1); err != nil {
		return errors.WithStack(err)
	}

	// CFA steps.

	// Output visualization of control flow analysis in HTML format.
	for i, prim := range prims {
		step := i + 1
		// Generate C visualization.
		if err := e.outputC(f.Name(), prim, step); err != nil {
			return errors.WithStack(err)
		}

		// Generate Go visualization.
		if err := e.highlightGo(f.Name(), step); err != nil {
			return errors.WithStack(err)
		}

		// Generate overview.
		if err := e.genOverview(funcName, step, nsteps); err != nil {
			return errors.WithStack(err)
		}
		// Generate control flow analysis visualization.
		htmlName := fmt.Sprintf("%s_cfa_%04d.html", funcName, step)
		htmlPath := filepath.Join(htmlDir, htmlName)
		htmlContent, err := e.genStep(f, prim, step, nsteps)
		if err != nil {
			return errors.WithStack(err)
		}
		dbg.Printf("creating file %q.", htmlPath)
		if err := ioutil.WriteFile(htmlPath, htmlContent, 0644); err != nil {
			return errors.WithStack(err)
		}
		if err := e.genLLVMHighlight(f, prim, step); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// genStep generates a visualization in HTML format of the intermediate step of
// the control flow analysis which recovered the control flow primitive of the
// given function.
func (e *Explorer) genStep(f *ir.Func, prim *primitive.Primitive, step, nsteps int) ([]byte, error) {
	llName := pathutil.FileName(e.LLPath)
	// TODO: embed cfa_step.tmpl in binary.
	srcDir, err := goutil.SrcDir("github.com/mewmew/explore/cmd/explore")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	tmplPath := filepath.Join(srcDir, "cfa_step.tmpl")
	ts, err := template.ParseFiles(tmplPath)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	t := ts.Lookup("cfa_step.tmpl")
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
func (e *Explorer) genLLVMHighlight(f *ir.Func, prim *primitive.Primitive, step int) error {
	// Get Chroma LLVM IR lexer.
	lexer := lexers.Get("llvm")
	if lexer == nil {
		lexer = lexers.Fallback
	}
	//lexer = chroma.Coalesce(lexer)
	// Get Chrome style.
	style := styles.Get(e.Style)
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
		html.WithClasses(),
		html.LineNumbersInTable(),
		html.HighlightLines(highlightRanges),
	)

	// Write CSS.
	htmlContent := &bytes.Buffer{}
	htmlContent.WriteString("<!DOCTYPE html><html><head><style>")
	if err := formatter.WriteCSS(htmlContent, style); err != nil {
		return errors.WithStack(err)
	}
	iterator, err := lexer.Tokenise(nil, f.LLString())
	if err != nil {
		return errors.WithStack(err)
	}
	htmlContent.WriteString("</style></head><body>")
	if err := formatter.Format(htmlContent, style, iterator); err != nil {
		return errors.WithStack(err)
	}
	htmlContent.WriteString("</body></html>")

	llvmHTMLName := fmt.Sprintf("%s_llvm_%04d.html", f.Name(), step)
	llvmHTMLPath := filepath.Join(e.OutputDir, llvmHTMLName)
	dbg.Printf("creating %q", llvmHTMLPath)
	if err := ioutil.WriteFile(llvmHTMLPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// genOverview generates an overview in HTML of the intermediate step of the
// decompilation.
func (e *Explorer) genOverview(funcName string, step, nsteps int) error {
	llName := pathutil.FileName(e.LLPath)
	// TODO: embed step_overview.tmpl in binary.
	srcDir, err := goutil.SrcDir("github.com/mewmew/explore/cmd/explore")
	if err != nil {
		return errors.WithStack(err)
	}
	tmplPath := filepath.Join(srcDir, "step_overview.tmpl")
	ts, err := template.ParseFiles(tmplPath)
	if err != nil {
		return errors.WithStack(err)
	}
	t := ts.Lookup("step_overview.tmpl")
	htmlContent := &bytes.Buffer{}
	var pages []int
	for page := 1; page <= nsteps; page++ {
		pages = append(pages, page)
	}
	data := map[string]interface{}{
		"Pages":    pages,
		"Step":     step,
		"Prev":     step - 1,
		"Next":     step + 1,
		"NSteps":   nsteps,
		"FuncName": funcName,
		"LLName":   llName,
	}
	if err := t.Execute(htmlContent, data); err != nil {
		return errors.WithStack(err)
	}
	// Store HTML file.
	overviewHTMLName := fmt.Sprintf("%s_%04d.html", funcName, step)
	overviewHTMLPath := filepath.Join(e.OutputDir, overviewHTMLName)
	dbg.Printf("creating %q", overviewHTMLPath)
	if err := ioutil.WriteFile(overviewHTMLPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// copyStyles copies the styles to the explore output directory.
func (e *Explorer) copyStyles() error {
	// Locate CSS files.
	srcPath, err := goutil.SrcDir("github.com/mewmew/explore/inc")
	if err != nil {
		return errors.WithStack(err)
	}
	// Copy CSS files.
	dstPath := filepath.Join(e.OutputDir, "inc")
	dbg.Printf("creating %q", dstPath)
	if err := dircopy.Copy(srcPath, dstPath); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// lineRangeOfBlock returns the line number range (1-based: [start, end]) of the
// basic block in the given function.
func lineRangeOfBlock(f *ir.Func, block *ir.Block) [2]int {
	funcStr := f.LLString()
	blockStr := block.LLString()
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
func (e *Explorer) createHTMLDir(force bool) (string, error) {
	if force {
		// Force overwrite existing graph directories.
		if err := os.RemoveAll(e.OutputDir); err != nil {
			return "", errors.WithStack(err)
		}
	}
	if err := os.Mkdir(e.OutputDir, 0755); err != nil {
		return "", errors.WithStack(err)
	}
	return e.OutputDir, nil
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

// highlightGo outputs a highlighted Go source file, highlighting the lines
// associated with the recovered control flow primitive.
func (e *Explorer) highlightGo(funcName string, step int) error {
	dotDir := pathutil.TrimExt(e.LLPath) + "_graphs"
	prims, err := parsePrims(dotDir, funcName)
	if err != nil {
		return errors.WithStack(err)
	}
	// TODO: add before and after stage; i.e. step_0001a and step_0001b.
	prims = prims[:step]
	goSource, err := e.decompGo(funcName, prims)
	if err != nil {
		return errors.WithStack(err)
	}
	//buf, err := ioutil.ReadFile(goPath)
	//if err != nil {
	//	return errors.WithStack(err)
	//}
	//goSource := string(buf)
	//// Get Chroma C lexer.
	lexer := lexers.Get("go")
	if lexer == nil {
		lexer = lexers.Fallback
	}
	//lexer = chroma.Coalesce(lexer)
	// Get Chrome style.
	style := styles.Get(e.Style)
	if style == nil {
		style = styles.Fallback
	}
	// Get Chroma HTML formatter.
	formatter := html.New(
		html.TabWidth(3),
		html.WithLineNumbers(),
		html.WithClasses(),
		html.LineNumbersInTable(),
		//html.HighlightLines(highlightRanges),
	)

	// Write CSS.
	htmlContent := &bytes.Buffer{}
	htmlContent.WriteString("<!DOCTYPE html><html><head><style>")
	if err := formatter.WriteCSS(htmlContent, style); err != nil {
		return errors.WithStack(err)
	}
	iterator, err := lexer.Tokenise(nil, goSource)
	if err != nil {
		return errors.WithStack(err)
	}
	htmlContent.WriteString("</style></head><body>")
	if err := formatter.Format(htmlContent, style, iterator); err != nil {
		return errors.WithStack(err)
	}
	htmlContent.WriteString("</body></html>")

	htmlName := fmt.Sprintf("%s_go_%04d.html", funcName, step)
	htmlPath := filepath.Join(e.OutputDir, htmlName)
	dbg.Printf("creating %q", htmlPath)
	if err := ioutil.WriteFile(htmlPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// decompGo decompiles the LLVM IR module into Go source code, based on the
// given recovered control flow primitives.
func (e *Explorer) decompGo(funcName string, prims []*primitive.Primitive) (string, error) {
	tmpDir, err := ioutil.TempDir("", "decomp-")
	if err != nil {
		return "", errors.WithStack(err)
	}
	newLLPath := filepath.Join(tmpDir, filepath.Base(e.LLPath))
	if err := dircopy.Copy(e.LLPath, newLLPath); err != nil {
		return "", errors.WithStack(err)
	}
	fmt.Println("tmpDir:", tmpDir)
	dotDir := filepath.Join(tmpDir, fmt.Sprintf("%s_graphs", pathutil.FileName(e.LLPath)))
	if err := os.MkdirAll(dotDir, 0755); err != nil {
		return "", errors.WithStack(err)
	}
	fmt.Println("dotDir:", dotDir)
	jsonPath := filepath.Join(dotDir, fmt.Sprintf("%s.json", funcName))
	if err := jsonutil.WriteFile(jsonPath, prims); err != nil {
		return "", errors.WithStack(err)
	}
	fmt.Println("jsonPath:", jsonPath)
	funcs := funcName
	cmd := exec.Command("ll2go2", "-funcs", funcs, newLLPath)
	buf := &bytes.Buffer{}
	cmd.Stdin = os.Stdin
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		return "", errors.WithStack(err)
	}
	fmt.Println(buf.String())
	return buf.String(), nil
}

// copyFile copies the source file to the destination path.
func copyFile(srcPath, dstPath string) error {
	buf, err := ioutil.ReadFile(srcPath)
	if err != nil {
		return errors.WithStack(err)
	}
	if err := ioutil.WriteFile(dstPath, buf, 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}
