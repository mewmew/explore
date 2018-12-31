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
	"github.com/llir/llvm/ir/metadata"
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
		if err := genVisualization(llPath, m, f, dotDir, htmlDir); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// genVisualization generates a visualization of the control flow analysis
// performed on the given function.
func genVisualization(llPath string, m *ir.Module, f *ir.Func, dotDir, htmlDir string) error {
	// Parse control flow primitives JSON file.
	funcName := f.Name()
	dbg.Printf("parsing primitives of function %q.", funcName)
	prims, err := parsePrims(dotDir, funcName)
	if err != nil {
		return errors.WithStack(err)
	}
	cPath := pathutil.TrimExt(llPath) + ".c"
	hasC := osutil.Exists(cPath)
	var cSource []byte
	if hasC {
		cSource, err = ioutil.ReadFile(cPath)
		if err != nil {
			return errors.WithStack(err)
		}
	}

	// Copy CSS include files.
	if err := copyStyles(llPath); err != nil {
		return errors.WithStack(err)
	}
	// Output visualization of control flow analysis in HTML format.
	for i, prim := range prims {
		step := i + 1
		nsteps := len(prims)
		if hasC {
			// Generate C visualization.
			if err := highlightC(llPath, f.Name(), prim, string(cSource), step, nsteps); err != nil {
				return errors.WithStack(err)
			}
		}

		// Generate Go visualization.
		if err := highlightGo(llPath, f.Name(), step, nsteps); err != nil {
			return errors.WithStack(err)
		}

		// Generate overview.
		if err := genOverview(llPath, funcName, step, nsteps); err != nil {
			return errors.WithStack(err)
		}
		// Generate control flow analysis visualization.
		htmlName := fmt.Sprintf("%s_cfa_%04d.html", funcName, step)
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
func genStep(llPath string, f *ir.Func, prim *primitive.Primitive, step, nsteps int) ([]byte, error) {
	llName := pathutil.FileName(llPath)
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
func genLLVMHighlight(llPath string, f *ir.Func, prim *primitive.Primitive, step int) error {
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

	exploreDir := pathutil.TrimExt(llPath) + "_explore"
	llvmHTMLName := fmt.Sprintf("%s_llvm_%04d.html", f.Name(), step)
	llvmHTMLPath := filepath.Join(exploreDir, llvmHTMLName)
	dbg.Printf("creating %q", llvmHTMLPath)
	if err := ioutil.WriteFile(llvmHTMLPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// genOverview generates an overview in HTML of the intermediate step of the
// decompilation.
func genOverview(llPath, funcName string, step, nsteps int) error {
	llName := pathutil.FileName(llPath)
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
	exploreDir := pathutil.TrimExt(llPath) + "_explore"
	overviewHTMLName := fmt.Sprintf("%s_%04d.html", funcName, step)
	overviewHTMLPath := filepath.Join(exploreDir, overviewHTMLName)
	dbg.Printf("creating %q", overviewHTMLPath)
	if err := ioutil.WriteFile(overviewHTMLPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// copyStyles copies the styles to the explore output directory.
func copyStyles(llPath string) error {
	// Locate CSS files.
	srcPath, err := goutil.SrcDir("github.com/mewmew/explore/inc")
	if err != nil {
		return errors.WithStack(err)
	}
	// Copy CSS files.
	exploreDir := pathutil.TrimExt(llPath) + "_explore"
	dstPath := filepath.Join(exploreDir, "inc")
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
func findBlock(f *ir.Func, blockName string) (*ir.Block, error) {
	for _, block := range f.Blocks {
		if block.Name() == blockName {
			return block, nil
		}
	}
	return nil, errors.Errorf("unable to locate basic block %q in function %q", blockName, f.Name())
}

// findFunc locates and returns the function with the specified name in the
// given module.
func findFunc(m *ir.Module, funcName string) (*ir.Func, error) {
	for _, f := range m.Funcs {
		if f.Name() == funcName {
			return f, nil
		}
	}
	return nil, errors.Errorf("unable to locate function %q in module", funcName)
}

// highlightCRanges returns line ranges within the C source code to highlight the
// lines associated with the basic block of the recovered control flow
// primitive.
func highlightCRanges(m *ir.Module, f *ir.Func, prim *primitive.Primitive) ([][2]int, error) {
	var highlightRanges [][2]int
	for _, blockName := range prim.Nodes {
		block, err := findBlock(f, blockName)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		highlightRange := lineRangeOfBlockInC(m, block)
		highlightRanges = append(highlightRanges, highlightRange...)
	}
	return highlightRanges, nil
}

type valueWithMetadata interface {
	MDAttachments() []*metadata.Attachment
}

// lineRangeOfBlockInC returns the line range of the given block, as based on
// the DILocation debug information of the instructions and terminator of that
// block.
func lineRangeOfBlockInC(m *ir.Module, block *ir.Block) [][2]int {
	var ranges [][2]int
	//var min, max int
	var vals []valueWithMetadata
	for _, inst := range block.Insts {
		vals = append(vals, inst.(valueWithMetadata))
	}
	vals = append(vals, block.Term.(valueWithMetadata))
	for _, val := range vals {
		for _, md := range val.MDAttachments() {
			if md.Name == "dbg" {
				if loc, ok := diLocation(md.Node); ok {
					line := int(loc.Line)
					//if min == 0 || line < min {
					//	min = line
					//}
					//if max == 0 || line > max {
					//	max = line
					//}
					r := [2]int{line, line}
					ranges = append(ranges, r)
					//min = 0
					//max = 0
				}
			}
		}
	}
	return ranges
}

// diLocation returns the DILocation specialized metadata node based on the
// given MDNode. The boolean return value indicates sucess.
func diLocation(node metadata.MDNode) (*metadata.DILocation, bool) {
	if n, ok := node.(*metadata.Def); ok {
		node = n.Node
	}
	if loc, ok := node.(*metadata.DILocation); ok {
		return loc, true
	}
	return nil, false
}

// highlightC outputs a highlighted C source file, highlighting the lines
// associated with the basic block of the recovered control flow primitive.
func highlightC(llPath string, funcName string, prim *primitive.Primitive, cSource string, step, nsteps int) error {
	llDbgPath := pathutil.TrimExt(llPath) + "_dbg.ll"
	m, err := asm.ParseFile(llDbgPath)
	if err != nil {
		return errors.WithStack(err)
	}
	f, err := findFunc(m, funcName)
	if err != nil {
		return errors.WithStack(err)
	}
	// Get Chroma C lexer.
	lexer := lexers.Get("c")
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
	highlightRanges, err := highlightCRanges(m, f, prim)
	if err != nil {
		return errors.WithStack(err)
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
	iterator, err := lexer.Tokenise(nil, cSource)
	if err != nil {
		return errors.WithStack(err)
	}
	htmlContent.WriteString("</style></head><body>")
	if err := formatter.Format(htmlContent, style, iterator); err != nil {
		return errors.WithStack(err)
	}
	htmlContent.WriteString("</body></html>")

	exploreDir := pathutil.TrimExt(llPath) + "_explore"
	cHTMLName := fmt.Sprintf("%s_c_%04d.html", f.Name(), step)
	cHTMLPath := filepath.Join(exploreDir, cHTMLName)
	dbg.Printf("creating %q", cHTMLPath)
	if err := ioutil.WriteFile(cHTMLPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// highlightGo outputs a highlighted Go source file, highlighting the lines
// associated with the recovered control flow primitive.
func highlightGo(llPath string, funcName string, step, nsteps int) error {
	dotDir := pathutil.TrimExt(llPath) + "_graphs"
	prims, err := parsePrims(dotDir, funcName)
	if err != nil {
		return errors.WithStack(err)
	}
	// TODO: add before and after stage; i.e. step_0001a and step_0001b.
	prims = prims[:step]
	goSource, err := decompGo(llPath, funcName, prims)
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
	// Get Chrome Monokai style.
	style := styles.Get("monokai")
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

	exploreDir := pathutil.TrimExt(llPath) + "_explore"
	goHTMLName := fmt.Sprintf("%s_go_%04d.html", funcName, step)
	goHTMLPath := filepath.Join(exploreDir, goHTMLName)
	dbg.Printf("creating %q", goHTMLPath)
	if err := ioutil.WriteFile(goHTMLPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// decompGo decompiles the LLVM IR module into Go source code, based on the
// given recovered control flow primitives.
func decompGo(llPath, funcName string, prims []*primitive.Primitive) (string, error) {
	tmpDir, err := ioutil.TempDir("", "decomp-")
	if err != nil {
		return "", errors.WithStack(err)
	}
	newLLPath := filepath.Join(tmpDir, filepath.Base(llPath))
	if err := dircopy.Copy(llPath, newLLPath); err != nil {
		return "", errors.WithStack(err)
	}
	fmt.Println("tmpDir:", tmpDir)
	dotDir := filepath.Join(tmpDir, fmt.Sprintf("%s_graphs", pathutil.FileName(llPath)))
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
