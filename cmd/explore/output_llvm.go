package main

import (
	"html/template"
	"path/filepath"

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
