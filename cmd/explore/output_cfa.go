package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"path/filepath"

	dircopy "github.com/otiai10/copy"
	"github.com/pkg/errors"
)

// parseCFATemplate parses the control flow analysis HTML template.
func (e *explorer) parseCFATemplate() error {
	tmplName := "cfa.tmpl"
	tmplPath := filepath.Join(e.repoDir, "cmd/explore", tmplName)
	ts, err := template.ParseFiles(tmplPath)
	if err != nil {
		return errors.WithStack(err)
	}
	e.cfaTmpl = ts.Lookup(tmplName)
	return nil
}

// outputCFA outputs the intermediate step of the control flow analysis,
// highlighting the nodes in the control flow graph associated with the basic
// blocks of the recovered control flow primitive.
//
// - funcName is the function name of the analyzed function.
//
// - step is the intermediate step of the control flow analysis.
//
// - subStep specifies whether the intermediate step is before or after merge,
//   where "a" specifies before and "b" after (using lexicographic naming to
//   have files be listed in the logical order).
func (e *explorer) outputCFA(funcName string, step int, subStep string) error {
	// Copy control flow graph.
	var cfgSrcName string
	switch step {
	case 0:
		cfgSrcName = fmt.Sprintf("%s.png", funcName)
	default:
		cfgSrcName = fmt.Sprintf("%s_%04d%s.png", funcName, step, subStep)
	}
	cfgSrcPath := filepath.Join(e.dotDir, cfgSrcName)
	cfgDstName := fmt.Sprintf("%s_step_%04d%s.png", funcName, step, subStep)
	cfgDstPath := filepath.Join(e.outputDir, "img", cfgDstName)
	dbg.Printf("creating file %q", cfgDstPath)
	dircopy.Copy(cfgSrcPath, cfgDstPath)
	// Output visualization of control flow analysis in HTML format.
	return e.outputCFAHTML(funcName, step, subStep)
}

// outputCFAHTML outputs the control flow analysis in HTML format, highlighting
// the nodes in the control flow graph associated with the basic blocks of the
// recovered control flow primitive.
//
// - funcName is the function name of the analyzed function.
//
// - step is the intermediate step of the control flow analysis.
//
// - subStep specifies whether the intermediate step is before or after merge,
//   where "a" specifies before and "b" after (using lexicographic naming to
//   have files be listed in the logical order).
func (e *explorer) outputCFAHTML(funcName string, step int, subStep string) error {
	// Description of intermediate step and substep.
	var desc string
	switch subStep {
	case "a":
		desc = fmt.Sprintf("Control flow graph of function %s, before merge in step %d.", funcName, step)
	case "b":
		desc = fmt.Sprintf("Control flow graph of function %s, after merge in step %d.", funcName, step)
	default:
		desc = fmt.Sprintf("Control flow graph of function %s.", funcName)
	}
	// Generate control flow analysis HTML page.
	htmlContent := &bytes.Buffer{}
	data := map[string]interface{}{
		"FuncName": funcName,
		"Step":     step,
		"SubStep":  subStep,
		"Desc":     desc,
	}
	if err := e.cfaTmpl.Execute(htmlContent, data); err != nil {
		return errors.WithStack(err)
	}
	htmlName := fmt.Sprintf("%s_step_%04d%s_cfa.html", funcName, step, subStep)
	htmlPath := filepath.Join(e.outputDir, htmlName)
	dbg.Printf("creating file %q", htmlPath)
	if err := ioutil.WriteFile(htmlPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}
