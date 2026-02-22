package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/hesusruiz/onboardng/common"
	"github.com/hesusruiz/onboardng/credissuance"
)

type Config struct {
	DestDir      string                         `yaml:"dest_dir"`
	SrcDir       string                         `yaml:"src_dir"`
	AppName      string                         `yaml:"app_name"`
	Environments map[string]credissuance.Config `yaml:"environments"`
}

func generate(cfg Config) {
	// Parse all layouts first
	layoutTmpl, err := template.New("").Funcs(template.FuncMap{
		"safe": func(s string) template.JS {
			b, _ := json.Marshal(s)
			return template.JS(b)
		},
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict expects an even number of arguments")
			}
			dict := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict keys must be strings")
				}
				dict[key] = values[i+1]
			}
			return dict, nil
		},
	}).ParseGlob(filepath.Join(cfg.SrcDir, "layouts/*.html"))
	if err != nil {
		slog.Error("❌ Layout Template Error", "error", err)
		return
	}

	// Find all pages
	pages, err := filepath.Glob(filepath.Join(cfg.SrcDir, "pages/*.html"))
	if err != nil {
		slog.Error("❌ Page Glob Error", "error", err)
		return
	}

	for envName, envVars := range cfg.Environments {
		targetDir := filepath.Join(cfg.DestDir, envName)
		os.MkdirAll(targetDir, 0755)

		if _, err := os.Stat(filepath.Join(cfg.SrcDir, "assets")); err == nil {
			copyDir(filepath.Join(cfg.SrcDir, "assets"), targetDir)
		}

		for _, page := range pages {
			pageBase := filepath.Base(page)

			// Clone the layout template so we don't pollute the shared one with this page's content
			tmpl, err := layoutTmpl.Clone()
			if err != nil {
				slog.Error("❌ Template Clone Error", "page", page, "error", err)
				continue
			}

			// Parse the specific page
			_, err = tmpl.ParseFiles(page)
			if err != nil {
				slog.Error("❌ Page Template Parse Error", "page", page, "error", err)
				continue
			}

			outputFile, _ := os.Create(filepath.Join(targetDir, pageBase))

			templateData := map[string]interface{}{
				"AppName":   cfg.AppName,
				"Env":       envName,
				"Vars":      envVars,
				"Countries": common.Countries,
			}

			// We execute "layout.html" which should include "content" (defined in the page)
			err = tmpl.ExecuteTemplate(outputFile, "layout.html", templateData)
			if err != nil {
				slog.Error("❌ Template Execution Error", "page", page, "error", err)
			}
			outputFile.Close()
		}
	}
	slog.Info("✅ Assets copied and HTML pages regenerated.")
}

// copyFile is a helper to move assets to the destination
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// copyDir recursively copies assets
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}
