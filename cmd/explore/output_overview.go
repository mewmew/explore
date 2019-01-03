package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"path/filepath"

	"github.com/alecthomas/chroma/styles"
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

// outputOverview outputs the overview of the visualization of the given
// function.
//
// - funcName is the function name of the analyzed function.
//
// - page is the page number of the visualization.
//
// - npages is the total number of pages.
//
// - subStep specifies whether the intermediate step is before or after merge,
//   where "a" specifies before and "b" after (using lexicographic naming to
//   have files be listed in the logical order).
func (e *explorer) outputOverview(funcName string, page, npages, step int, subStep string) error {
	// Generate Overview HTML page.
	htmlContent := &bytes.Buffer{}
	var pages []int
	for i := 1; i <= npages; i++ {
		pages = append(pages, i)
	}
	data := map[string]interface{}{
		"FuncName": funcName,
		"Style":    e.style,
		"Styles":   styles.Names(),
		"Pages":    pages,
		"PrevPage": page - 1,
		"CurPage":  page,
		"NextPage": page + 1,
		"NPages":   npages,
		"Step":     step,
		"SubStep":  subStep,
	}
	if err := e.overviewTmpl.Execute(htmlContent, data); err != nil {
		return errors.WithStack(err)
	}
	htmlName := fmt.Sprintf("%s_%04d.html", funcName, page)
	htmlPath := filepath.Join(e.outputDir, htmlName)
	dbg.Printf("creating file %q", htmlPath)
	if err := ioutil.WriteFile(htmlPath, htmlContent.Bytes(), 0644); err != nil {
		return errors.WithStack(err)
	}
	return nil
}
