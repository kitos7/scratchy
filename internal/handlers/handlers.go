// Package handlers заводит файлы gRPC-ручек по proto-контрактам проекта:
// пакет на proto-сервис, файл на rpc. Существующий код не трогается.
package handlers

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"

	"github.com/nikita/scratch/internal/generator"
)

// serverDir — корень транспорта в сгенерированном проекте.
const serverDir = "internal/server"

// Report — итог прогона: что создано, что пропущено и что доделать руками.
type Report struct {
	// Created — созданные файлы, пути относительно корня проекта.
	Created []string
	// Existing — ручки, у которых метод уже реализован: "DemoService.Echo".
	Existing []string
	// NewServices — сервисы, для которых пришлось создать пакет:
	// их ещё нужно зарегистрировать в DI.
	NewServices []Service
	// Warnings — то, что генератор сделать не смог.
	Warnings []string
}

// Generate разбирает proto-контракты проекта в dir и дописывает
// недостающие файлы ручек. Существующие файлы не перезаписываются.
func Generate(dir string) (*Report, error) {
	manifest, err := generator.LoadManifest(dir)
	if err != nil {
		return nil, err
	}

	services, err := parseServices(dir, manifest.Params.Package)
	if err != nil {
		return nil, err
	}

	report := &Report{}
	for _, svc := range services {
		if err := generateService(dir, manifest.Params.Module, svc, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

// generateService заводит пакет сервиса (если его ещё нет) и файлы ручек.
func generateService(dir, module string, svc Service, report *Report) error {
	pkgDir := filepath.Join(dir, filepath.FromSlash(serverDir), svc.Pkg)
	serverFile := filepath.Join(pkgDir, "server.go")

	if _, err := os.Stat(serverFile); os.IsNotExist(err) {
		content, err := renderServer(svc, module)
		if err != nil {
			return err
		}
		if err := writeFile(serverFile, content); err != nil {
			return err
		}
		report.Created = append(report.Created, relPath(dir, serverFile))
		report.NewServices = append(report.NewServices, svc)
	} else if err != nil {
		return fmt.Errorf("проверка %s: %w", serverFile, err)
	}

	implemented, err := serverMethods(pkgDir)
	if err != nil {
		return err
	}

	for _, rpc := range svc.RPCs {
		switch {
		case implemented[rpc.Name]:
			report.Existing = append(report.Existing, svc.Name+"."+rpc.Name)
			continue
		case rpc.Foreign:
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"%s.%s: %s/%s объявлены в другом proto-пакете — заведи ручку вручную",
				svc.Name, rpc.Name, rpc.Request, rpc.Response))
			continue
		}

		target := filepath.Join(pkgDir, fileName(rpc.Name))
		if _, err := os.Stat(target); err == nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"%s.%s: файл %s уже есть, но метода в пакете нет — допиши ручку вручную",
				svc.Name, rpc.Name, relPath(dir, target)))
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("проверка %s: %w", target, err)
		}

		content, err := renderHandler(svc, rpc, module)
		if err != nil {
			return err
		}
		if err := writeFile(target, content); err != nil {
			return err
		}
		report.Created = append(report.Created, relPath(dir, target))
	}
	return nil
}

// serverMethods собирает имена методов с ресивером Server во всём пакете:
// ручка считается реализованной независимо от того, в каком файле лежит.
func serverMethods(pkgDir string) (map[string]bool, error) {
	methods := map[string]bool{}

	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, fmt.Errorf("чтение пакета %s: %w", pkgDir, err)
	}

	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(pkgDir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("разбор %s: %w", filepath.Join(pkgDir, e.Name()), err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			if receiverName(fn.Recv.List[0].Type) == "Server" {
				methods[fn.Name.Name] = true
			}
		}
	}
	return methods, nil
}

// receiverName достаёт имя типа ресивера: Server и *Server → Server.
func receiverName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// DIHint — сниппет для регистрации нового сервиса: DI scratch не правит.
func DIHint(svc Service) string {
	return fmt.Sprintf(`  internal/di/providers.go — параметр newApp: %sSrv *%s.Server
    в app.WithGRPC:      %s.Register%sServer(s, %sSrv)
    рядом с опциями:     app.WithGateway(%s.Register%sHandler),
  internal/di/wire.go — в wire.Build: %s.New`,
		svc.Pkg, svc.Pkg, svc.Alias, svc.Name, svc.Pkg, svc.Alias, svc.Name, svc.Pkg)
}

// Sorted возвращает предупреждения в стабильном порядке.
func (r *Report) Sorted() []string {
	out := append([]string(nil), r.Warnings...)
	sort.Strings(out)
	return out
}

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir для %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("запись %s: %w", path, err)
	}
	return nil
}

func relPath(dir, path string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
