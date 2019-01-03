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
	"github.com/llir/llvm/ir"
	"github.com/mewkiz/pkg/goutil"
	"github.com/mewkiz/pkg/jsonutil"
	"github.com/mewkiz/pkg/osutil"
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
	// Parse control flow primitives JSON file.
	funcName := f.Name()
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
		if err := e.outputOverview(funcName, page, npages, step); err != nil {
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
		// Output original LLVM IR assembly.
		if err := e.outputLLVM(funcName, prim, step); err != nil {
			return errors.WithStack(err)
		}
	}

	// LLVM IR assembly.
	// TODO: output LLVM IR.

	// Control flow graph.
	// TODO: output CFG.

	// Reconstructed Go source code.
	// TODO: output Go.

	// First overview.
	//if err := e.highlightGo(f.Name(), 1); err != nil {
	//	return errors.WithStack(err)
	//}

	// CFA steps.

	// Output visualization of control flow analysis in HTML format.
	/*
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
			dbg.Printf("creating file %q", htmlPath)
			if err := ioutil.WriteFile(htmlPath, htmlContent, 0644); err != nil {
				return errors.WithStack(err)
			}
			if err := e.genLLVMHighlight(f, prim, step); err != nil {
				return errors.WithStack(err)
			}
		}
	*/
	return nil
}

// genStep generates a visualization in HTML format of the intermediate step of
// the control flow analysis which recovered the control flow primitive of the
// given function.
func (e *explorer) genStep(f *ir.Func, prim *primitive.Primitive, step, nsteps int) ([]byte, error) {
	llName := pathutil.FileName(e.llPath)
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

// genOverview generates an overview in HTML of the intermediate step of the
// decompilation.
func (e *explorer) genOverview(funcName string, step, nsteps int) error {
	llName := pathutil.FileName(e.llPath)
	// TODO: embed step_overview.tmpl in binary.
	tmplPath := filepath.Join(e.repoDir, "step_overview.tmpl")
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
	overviewHTMLPath := filepath.Join(e.outputDir, overviewHTMLName)
	dbg.Printf("creating file %q", overviewHTMLPath)
	if err := ioutil.WriteFile(overviewHTMLPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// highlightGo outputs a highlighted Go source file, highlighting the lines
// associated with the recovered control flow primitive.
func (e *explorer) highlightGo(funcName string, step int) error {
	prims, err := e.parsePrims(funcName)
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
	htmlPath := filepath.Join(e.outputDir, htmlName)
	dbg.Printf("creating file %q", htmlPath)
	if err := ioutil.WriteFile(htmlPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// decompGo decompiles the LLVM IR module into Go source code, based on the
// given recovered control flow primitives.
func (e *explorer) decompGo(funcName string, prims []*primitive.Primitive) (string, error) {
	tmpDir, err := ioutil.TempDir("", "decomp-")
	if err != nil {
		return "", errors.WithStack(err)
	}
	newLLPath := filepath.Join(tmpDir, filepath.Base(e.llPath))
	if err := dircopy.Copy(e.llPath, newLLPath); err != nil {
		return "", errors.WithStack(err)
	}
	fmt.Println("tmpDir:", tmpDir)
	dotDir := filepath.Join(tmpDir, fmt.Sprintf("%s_graphs", pathutil.FileName(e.llPath)))
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
