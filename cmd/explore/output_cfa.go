package main

import (
	"html/template"
	"path/filepath"

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
