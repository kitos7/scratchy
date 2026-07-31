// Package generator рендерит шаблоны проекта и управляет манифестом
// .scratch.yaml для последующих обновлений.
package generator

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

//go:embed all:templates
var templatesFS embed.FS

const templateRoot = "templates/project"

// managedFiles — файлы тулинга, которыми scratch владеет и обновляет
// командой `scratch update`. Код приложения scratch не трогает никогда.
var managedFiles = map[string]bool{
	"Makefile":                 true,
	"buf.yaml":                 true,
	"buf.gen.yaml":             true,
	".golangci.yml":            true,
	".mockery.yaml":            true,
	"Dockerfile":               true,
	"docker-compose.yml":       true,
	".github/workflows/ci.yml": true,
	".gitignore":               true,
}

// Result — итог генерации.
type Result struct {
	Dir      string
	Files    []string
	Warnings []string
}

// Render рендерит все шаблоны проекта в память: относительный путь → содержимое.
func Render(p Params) (map[string][]byte, error) {
	files := map[string][]byte{}

	err := fs.WalkDir(templatesFS, templateRoot, func(pathInFS string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		raw, err := templatesFS.ReadFile(pathInFS)
		if err != nil {
			return fmt.Errorf("read template %s: %w", pathInFS, err)
		}

		rel := strings.TrimPrefix(pathInFS, templateRoot+"/")
		rel = strings.TrimSuffix(rel, ".tmpl")
		rel = strings.ReplaceAll(rel, "__app__", p.AppName)
		rel = strings.ReplaceAll(rel, "__package__", p.Package)
		rel = strings.ReplaceAll(rel, "__serverpkg__", p.ServerPkg)

		tpl, err := template.New(rel).Delims("[[", "]]").Parse(string(raw))
		if err != nil {
			return fmt.Errorf("parse template %s: %w", pathInFS, err)
		}
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, p); err != nil {
			return fmt.Errorf("render template %s: %w", pathInFS, err)
		}

		files[rel] = buf.Bytes()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// Generate рендерит проект в dir, пишет манифест и возвращает отчёт.
func Generate(dir string, p Params, force bool) (*Result, error) {
	if err := ensureTargetDir(dir, force); err != nil {
		return nil, err
	}

	files, err := Render(p)
	if err != nil {
		return nil, err
	}

	res := &Result{Dir: dir}
	manifest := NewManifest(p)

	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir for %s: %w", rel, err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", rel, err)
		}
		res.Files = append(res.Files, rel)
		if managedFiles[rel] {
			manifest.Managed[rel] = hashBytes(content)
		}
	}

	manifest.GeneratedAt = time.Now().UTC()
	if err := manifest.Save(dir); err != nil {
		return nil, err
	}

	if p.LibVersion == placeholderVersion && p.LibReplace == "" {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("scratch собран без semver-версии (%s): в go.mod указан placeholder для %s — добавь replace на локальную копию либы или сгенерируй с флагом --lib-replace", p.ScratchVersion, p.ScratchModule))
	}

	return res, nil
}

func ensureTargetDir(dir string, force bool) error {
	entries, err := os.ReadDir(dir)
	switch {
	case os.IsNotExist(err):
		return os.MkdirAll(dir, 0o755)
	case err != nil:
		return fmt.Errorf("read target dir: %w", err)
	case len(entries) > 0 && !force:
		return fmt.Errorf("директория %s не пуста (используй --force, чтобы перезаписать)", dir)
	}
	return nil
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
