package handlers

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	protoparse "github.com/emicklei/proto"

	"github.com/kitos7/scratchy/internal/generator"
)

// Service — proto-сервис, найденный в контрактах проекта.
type Service struct {
	// Name — имя сервиса из proto (DemoService).
	Name string
	// GoName — как это имя выглядит в сгенерированном Go-коде: protoc-gen-go
	// мангли́т proto-имена, и ссылаться в коде надо именно на этот вариант.
	GoName string
	// Pkg — Go-пакет транспорта (demoservice).
	Pkg string
	// ImportPath — импорт сгенерированного pb-пакета.
	ImportPath string
	// Alias — алиас импорта pb-пакета (demov1).
	Alias string
	// ProtoFile — путь к .proto относительно корня проекта (для сообщений).
	ProtoFile string
	// RPCs — ручки сервиса в порядке объявления.
	RPCs []RPC
}

// RPC — одна ручка сервиса.
type RPC struct {
	// Name — имя rpc из proto.
	Name string
	// GoName — имя метода в сгенерированном Go-коде.
	GoName string
	// Request/Response — имена типов сообщений из proto, без пакета.
	Request  string
	Response string
	// GoRequest/GoResponse — те же типы, как их назвал protoc-gen-go.
	GoRequest  string
	GoResponse string
	// StreamRequest/StreamResponse — флаги стриминга.
	StreamRequest  bool
	StreamResponse bool
	// Foreign — тип сообщения объявлен в другом proto-пакете:
	// такую ручку сгенерировать не получится.
	Foreign bool
}

// Streaming сообщает, что ручка использует стриминг в любую сторону.
func (r RPC) Streaming() bool { return r.StreamRequest || r.StreamResponse }

// parseServices собирает сервисы из всех proto-контрактов проекта.
// Вендоренные контракты (api/proto/google/...) пропускаются.
func parseServices(dir string) ([]Service, error) {
	root := filepath.Join(dir, "api", "proto")
	vendored := filepath.Join(root, "google")
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path == vendored {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".proto") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("обход %s: %w", root, err)
	}
	sort.Strings(files)

	var services []Service
	for _, f := range files {
		svc, err := parseFile(dir, f)
		if err != nil {
			return nil, err
		}
		services = append(services, svc...)
	}
	return services, nil
}

// parseFile разбирает один .proto: go_package + сервисы с их rpc.
func parseFile(dir, path string) ([]Service, error) {
	f, err := os.Open(path) //nolint:gosec // путь собирается из корня проекта
	if err != nil {
		return nil, fmt.Errorf("открыть %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	def, err := protoparse.NewParser(f).Parse()
	if err != nil {
		return nil, fmt.Errorf("разбор %s: %w", path, err)
	}

	rel, err := filepath.Rel(dir, path)
	if err != nil {
		rel = path
	}

	var goPackage string
	var services []Service

	protoparse.Walk(def,
		protoparse.WithOption(func(o *protoparse.Option) {
			if o.Name == "go_package" {
				goPackage = o.Constant.Source
			}
		}),
		protoparse.WithService(func(s *protoparse.Service) {
			svc := Service{
				Name:      s.Name,
				GoName:    generator.GoCamelCase(s.Name),
				Pkg:       generator.ServerPackage(s.Name),
				ProtoFile: filepath.ToSlash(rel),
			}
			for _, el := range s.Elements {
				rpc, ok := el.(*protoparse.RPC)
				if !ok {
					continue
				}
				foreign := strings.Contains(rpc.RequestType, ".") || strings.Contains(rpc.ReturnsType, ".")
				svc.RPCs = append(svc.RPCs, RPC{
					Name:           rpc.Name,
					GoName:         generator.GoCamelCase(rpc.Name),
					Request:        rpc.RequestType,
					Response:       rpc.ReturnsType,
					GoRequest:      generator.GoCamelCase(rpc.RequestType),
					GoResponse:     generator.GoCamelCase(rpc.ReturnsType),
					StreamRequest:  rpc.StreamsRequest,
					StreamResponse: rpc.StreamsReturns,
					Foreign:        foreign,
				})
			}
			services = append(services, svc)
		}),
	)

	if goPackage == "" && len(services) > 0 {
		return nil, fmt.Errorf("%s: нет option go_package — не могу определить импорт pb-пакета", rel)
	}

	importPath, alias := splitGoPackage(goPackage)
	for i := range services {
		services[i].ImportPath = importPath
		services[i].Alias = alias
	}
	return services, nil
}

// splitGoPackage разбирает go_package вида "module/gen/demo/v1;demov1"
// на импорт и алиас; без явного алиаса берётся последний сегмент пути.
func splitGoPackage(v string) (importPath, alias string) {
	importPath = v
	if i := strings.LastIndex(v, ";"); i >= 0 {
		importPath, alias = v[:i], v[i+1:]
	}
	if alias == "" {
		alias = path.Base(importPath)
	}
	return importPath, alias
}
