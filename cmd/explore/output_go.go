package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/alecthomas/chroma/formatters/html"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/mewkiz/pkg/jsonutil"
	"github.com/mewkiz/pkg/pathutil"
	"github.com/mewmew/lnp/pkg/cfa/primitive"
	dircopy "github.com/otiai10/copy"
	"github.com/pkg/errors"
)

// parseGoTemplate parses the Go HTML template.
func (e *explorer) parseGoTemplate() error {
	tmplName := "go.tmpl"
	tmplPath := filepath.Join(e.repoDir, "cmd/explore", tmplName)
	ts, err := template.ParseFiles(tmplPath)
	if err != nil {
		return errors.WithStack(err)
	}
	e.goTmpl = ts.Lookup(tmplName)
	return nil
}

// outputGo outputs the reconstructed Go source code, highlighting the lines of
// the recovered control flow primitive.
//
// - funcName is the function name of the analyzed function.
//
// - prims is the list of recovered control flow primitives.
//
// - step is the intermediate step of the control flow analysis.
//
// - subStep specifies whether the intermediate step is before or after merge,
//   where "a" specifies before and "b" after (using lexicographic naming to
//   have files be listed in the logical order).
func (e *explorer) outputGo(funcName string, prims []*primitive.Primitive, step int, subStep string) error {
	// Decompile LLVM IR assembly into Go source code.
	var stepPrims []*primitive.Primitive
	switch subStep {
	case "a":
		// Before merge.
		stepPrims = prims[:step-1]
	case "b":
		// After merge.
		stepPrims = prims[:step]
	default:
		// nothing to do.
	}
	goSource, err := e.decompGo(funcName, stepPrims)
	if err != nil {
		return errors.WithStack(err)
	}
	var lines [][2]int
	// TODO: calculate lines to highlight.
	return e.outputGoHTML(goSource, funcName, lines, step, subStep)
}

// outputGoHTML outputs the recovered Go source code in HTML format,
// highlighting the specified lines.
//
// - goSource is the contents of the recovered Go source code.
//
// - funcName is the function name of the analyzed function.
//
// - lines is the list of lines to highlight.
//
// - step is the intermediate step of the control flow analysis.
//
// - subStep specifies whether the intermediate step is before or after merge,
//   where "a" specifies before and "b" after (using lexicographic naming to
//   have files be listed in the logical order).
func (e *explorer) outputGoHTML(goSource, funcName string, lines [][2]int, step int, subStep string) error {
	// Get Chroma Go lexer.
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
		//html.HighlightLines(lines),
	)
	// Generate syntax highlighted Go code.
	iterator, err := lexer.Tokenise(nil, goSource)
	if err != nil {
		return errors.WithStack(err)
	}
	goCode := &bytes.Buffer{}
	if err := formatter.Format(goCode, style, iterator); err != nil {
		return errors.WithStack(err)
	}
	// Generate Go HTML page.
	htmlContent := &bytes.Buffer{}
	data := map[string]interface{}{
		"FuncName": funcName,
		"Style":    e.style,
		"GoCode":   template.HTML(goCode.String()),
	}
	if err := e.goTmpl.Execute(htmlContent, data); err != nil {
		return errors.WithStack(err)
	}
	htmlName := fmt.Sprintf("%s_step_%04d%s_go.html", funcName, step, subStep)
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
	// Create temporary directory used for decompilation.
	tmpDir, err := ioutil.TempDir("", "decomp-")
	if err != nil {
		return "", errors.WithStack(err)
	}
	defer os.RemoveAll(tmpDir)
	// Copy LLVM IR assembly file to temp dir.
	tmpLLPath := filepath.Join(tmpDir, filepath.Base(e.llPath))
	if err := dircopy.Copy(e.llPath, tmpLLPath); err != nil {
		return "", errors.WithStack(err)
	}
	// Write prims in JSON format to temp dir.
	llName := pathutil.FileName(e.llPath)
	tmpDotDir := filepath.Join(tmpDir, fmt.Sprintf("%s_graphs", llName))
	if err := os.MkdirAll(tmpDotDir, 0755); err != nil {
		return "", errors.WithStack(err)
	}
	jsonName := fmt.Sprintf("%s.json", funcName)
	jsonPath := filepath.Join(tmpDotDir, jsonName)
	if err := jsonutil.WriteFile(jsonPath, prims); err != nil {
		return "", errors.WithStack(err)
	}
	// Execute decompiler command.
	funcs := funcName
	cmd := exec.Command("ll2go2", "-funcs", funcs, tmpLLPath)
	buf := &bytes.Buffer{}
	cmd.Stdin = os.Stdin
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr
	// Set current working directory to temp dir.
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		return "", errors.WithStack(err)
	}
	return buf.String(), nil
}
