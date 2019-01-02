package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"path/filepath"

	"github.com/alecthomas/chroma/formatters/html"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/metadata"
	"github.com/mewkiz/pkg/osutil"
	"github.com/mewkiz/pkg/pathutil"
	"github.com/mewmew/lnp/pkg/cfa/primitive"
	"github.com/pkg/errors"
)

// parseCTemplate parses the C HTML template.
func (e *explorer) parseCTemplate() error {
	tmplName := "c.tmpl"
	tmplPath := filepath.Join(e.repoDir, "cmd/explore", tmplName)
	ts, err := template.ParseFiles(tmplPath)
	if err != nil {
		return errors.WithStack(err)
	}
	e.cTmpl = ts.Lookup(tmplName)
	return nil
}

// parseC parses the original C source file.
func (e *explorer) parseC() (string, error) {
	// Locate original C source file.
	m := e.m
	if e.dbg != nil {
		m = e.dbg
	}
	cPath, ok := findCPath(e.llPath, m)
	if !ok {
		// Early exit if original C source file is not present.
		return "", nil
	}
	dbg.Printf("reading file %q", cPath)
	buf, err := ioutil.ReadFile(cPath)
	if err != nil {
		return "", errors.WithStack(err)
	}
	cSource := string(buf)
	return cSource, nil
}

// outputC outputs the original C source file of the LLVM IR assembly,
// highlighting the lines in the given function associated with the basic blocks
// of the recovered control flow primitive.
//
// - cSource is the contents of the original C source code.
//
// - funcName is the function name of the analyzed function.
//
// - prim is the recovered control flow primitives; or nil if not present.
//
// - page is the page number of the visualization.
func (e *explorer) outputC(cSource, funcName string, prim *primitive.Primitive, page int) error {
	// Locate lines to highlight of control flow primitive.
	var lines [][2]int
	if prim != nil {
		f, err := findFunc(e.m, funcName)
		if err != nil {
			return errors.WithStack(err)
		}
		lines, err = findCHighlight(f, prim)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	return e.outputCHTML(cSource, funcName, lines, page)
}

// outputCHTML outputs the C source code in HTML format, highlighting the specified lines.
//
// - cSource is the contents of the original C source code.
//
// - funcName is the function name of the analyzed function.
//
// - lines is the list of lines to highlight.
//
// - page is the page number of the visualization.
func (e *explorer) outputCHTML(cSource, funcName string, lines [][2]int, page int) error {
	// Get Chroma C lexer.
	lexer := lexers.Get("c")
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
	// Generate syntax highlighted C code.
	iterator, err := lexer.Tokenise(nil, cSource)
	if err != nil {
		return errors.WithStack(err)
	}
	cCode := &bytes.Buffer{}
	if err := formatter.Format(cCode, style, iterator); err != nil {
		return errors.WithStack(err)
	}
	// Generate C HTML page.
	htmlContent := &bytes.Buffer{}
	data := map[string]interface{}{
		"Func":   funcName,
		"Style":  e.style,
		"Styles": styles.Names(),
		"CCode":  template.HTML(cCode.String()),
	}
	if err := e.cTmpl.Execute(htmlContent, data); err != nil {
		return errors.WithStack(err)
	}
	htmlName := fmt.Sprintf("%s_c_%04d.html", funcName, page)
	htmlPath := filepath.Join(e.outputDir, htmlName)
	dbg.Printf("creating %q", htmlPath)
	if err := ioutil.WriteFile(htmlPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// findCPath tries to locate the path of the original C source file used to
// produce the given LLVM IR module. It tries to locate the C source file
// firstly based on the DWARF metadata debug info DIFile of the parsed module,
// secondly based on the source_filename top-level entity of the parsed module,
// and lastly based on the LLVM IR assembly path.
//
// - llPath is the path to the LLVM IR assembly file.
//
// - m is the parsed LLVM IR module (or the parsed debug module if present).
func findCPath(llPath string, m *ir.Module) (string, bool) {
	if md, ok := m.NamedMetadataDefs["llvm.dbg.cu"]; ok {
		unit := md.Nodes[0].(*metadata.DICompileUnit)
		cPath := filepath.Join(unit.File.Directory, unit.File.Filename)
		return cPath, true
	}
	if len(m.SourceFilename) > 0 && osutil.Exists(m.SourceFilename) {
		fmt.Println("source_filename")
		return m.SourceFilename, true
	}
	cPath := pathutil.TrimExt(llPath) + ".c"
	if osutil.Exists(cPath) {
		fmt.Println("exists")
		return cPath, true
	}
	return "", false
}

// findCHighlight returns the lines to highlight in the given function
// associated with the basic blocks of the recovered control flow primitive.
func findCHighlight(f *ir.Func, prim *primitive.Primitive) ([][2]int, error) {
	var lines [][2]int
	for _, blockName := range prim.Nodes {
		block, err := findBlock(f, blockName)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		lines = append(lines, findBlockLines(block)...)
	}
	return lines, nil
}

// valueWithMetadata is the interface implemented by values with metadata
// attachments.
type valueWithMetadata interface {
	// MDAttachments returns the metadata attachments of the value.
	MDAttachments() []*metadata.Attachment
}

// findBlockLines returns the lines of the given block, as based on the
// DILocation debug information of the instructions and terminator of the block.
func findBlockLines(block *ir.Block) [][2]int {
	var lines [][2]int
	for _, inst := range block.Insts {
		if line, ok := findLine(inst.(valueWithMetadata)); ok {
			lines = append(lines, line)
		}
	}
	if line, ok := findLine(block.Term.(valueWithMetadata)); ok {
		lines = append(lines, line)
	}
	return lines
}

// findLine returns the line of the given value, as based on the DILocation
// debug information of its metadata attachments. The boolean return value
// indicates success.
func findLine(v valueWithMetadata) ([2]int, bool) {
	for _, md := range v.MDAttachments() {
		if md.Name == "dbg" {
			if loc, ok := md.Node.(*metadata.DILocation); ok {
				line := int(loc.Line)
				return [2]int{line, line}, true
			}
		}
	}
	return [2]int{}, false
}
