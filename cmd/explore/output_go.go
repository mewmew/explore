package main

import (
	"html/template"
	"path/filepath"

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