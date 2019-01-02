package main

import (
	"html/template"
	"path/filepath"

	"github.com/pkg/errors"
)

// parseOverviewTemplate parses the overview HTML template.
func (e *explorer) parseOverviewTemplate() error {
	tmplName := "overview.tmpl"
	tmplPath := filepath.Join(e.repoDir, "cmd/explore", tmplName)
	ts, err := template.ParseFiles(tmplPath)
	if err != nil {
		return errors.WithStack(err)
	}
	e.overviewTmpl = ts.Lookup(tmplName)
	return nil
}
