package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/formatters/html"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/llir/llvm/ir"
	"github.com/mewmew/lnp/pkg/cfa/primitive"
	"github.com/pkg/errors"
)

// parseLLVMTemplate parses the LLVM HTML template.
func (e *explorer) parseLLVMTemplate() error {
	tmplName := "llvm.tmpl"
	tmplPath := filepath.Join(e.repoDir, "cmd/explore", tmplName)
	ts, err := template.ParseFiles(tmplPath)
	if err != nil {
		return errors.WithStack(err)
	}
	e.llvmTmpl = ts.Lookup(tmplName)
	return nil
}

// outputLLVM outputs the LLVM IR assembly file, highlighting the lines in the
// given function associated with the basic blocks of the recovered control flow
// primitive.
//
// - funcName is the function name of the analyzed function.
//
// - prim is the recovered control flow primitives; or nil if not present.
//
// - step is the intermediate step of the control flow analysis.
func (e *explorer) outputLLVM(funcName string, prim *primitive.Primitive, step int) error {
	// Locate lines to highlight of control flow primitive.
	var lines [][2]int
	f, err := findFunc(e.m, funcName)
	if err != nil {
		return errors.WithStack(err)
	}
	if prim != nil {
		lines, err = findLLVMHighlight(f, prim)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	return e.outputLLVMHTML(f, lines, step)
}

// outputLLVMHTML outputs the LLVM IR assembly in HTML format, highlighting the
// specified lines.
//
// - f is the function to visualize.
//
// - lines is the list of lines to highlight.
//
// - step is the intermediate step of the control flow analysis.
func (e *explorer) outputLLVMHTML(f *ir.Func, lines [][2]int, step int) error {
	// Get Chroma LLVM IR lexer.
	lexer := lexers.Get("llvm")
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
		html.HighlightLines(lines),
	)
	// Generate syntax highlighted LLVM IR assembly.
	llvmSource := f.LLString()
	iterator, err := lexer.Tokenise(nil, llvmSource)
	if err != nil {
		return errors.WithStack(err)
	}
	llvmCode := &bytes.Buffer{}
	if err := formatter.Format(llvmCode, style, iterator); err != nil {
		return errors.WithStack(err)
	}
	// Generate LLVM IR HTML page.
	htmlContent := &bytes.Buffer{}
	funcName := f.Name()
	data := map[string]interface{}{
		"FuncName": funcName,
		"Style":    e.style,
		"LLVMCode": template.HTML(llvmCode.String()),
	}
	if err := e.llvmTmpl.Execute(htmlContent, data); err != nil {
		return errors.WithStack(err)
	}
	htmlName := fmt.Sprintf("%s_step_%04d_llvm.html", funcName, step)
	htmlPath := filepath.Join(e.outputDir, htmlName)
	dbg.Printf("creating file %q", htmlPath)
	if err := ioutil.WriteFile(htmlPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// findLLVMHighlight returns the line ranges to highlight in the given function
// associated with the basic blocks of the recovered control flow primitive.
func findLLVMHighlight(f *ir.Func, prim *primitive.Primitive) ([][2]int, error) {
	// Line number ranges to highlight (1-based line numbers, inclusive).
	var lineRanges [][2]int
	for _, blockName := range prim.Nodes {
		block, err := findBlock(f, blockName)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		lineRange := findBlockLineRange(f, block)
		lineRanges = append(lineRanges, lineRange)
	}
	return lineRanges, nil
}

// findBlockLineRange returns the line range (1-based: [start, end]) of the
// basic block in the given function.
func findBlockLineRange(f *ir.Func, block *ir.Block) [2]int {
	funcStr := f.LLString()
	blockStr := block.LLString()
	pos := strings.Index(funcStr, blockStr)
	if pos == -1 {
		panic(fmt.Errorf("unable to locate contents of basic block %s in contents of function %s", block.Ident(), f.Ident()))
	}
	before := funcStr[:pos]
	start := 1 + strings.Count(before, "\n")
	n := strings.Count(blockStr, "\n")
	end := start + n
	return [2]int{start, end}
}
