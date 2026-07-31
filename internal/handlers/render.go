package handlers

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"
	"text/template"
	"unicode"
)

// serverTemplate — каркас транспорта нового сервиса: тип и конструктор.
// Зависимости у нового сервиса те же, что у сгенерированного скелета:
// дальше их правит разработчик.
var serverTemplate = template.Must(template.New("server").Parse(`// Package {{.Svc.Pkg}} — gRPC-транспорт сервиса {{.Svc.Name}}:
// одна ручка — один файл (файлы ручек заводит ` + "`scratch handlers`" + `).
package {{.Svc.Pkg}}

import (
	{{.Svc.Alias}} "{{.Svc.ImportPath}}"
	"{{.Module}}/internal/service"
)

// Server реализует {{.Svc.Name}}.
type Server struct {
	{{.Svc.Alias}}.Unimplemented{{.Svc.GoName}}Server

	svc *service.Service
}

// New создаёт gRPC-хендлер сервиса.
func New(svc *service.Service) *Server {
	return &Server{svc: svc}
}
`))

// handlerTemplate — заготовка одной ручки.
var handlerTemplate = template.Must(template.New("handler").Parse(`package {{.Svc.Pkg}}

import (
{{- if not .RPC.Streaming}}
	"context"
{{end}}
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	{{.Svc.Alias}} "{{.Svc.ImportPath}}"
)

// {{.RPC.Name}} — заготовка ручки, сгенерированная scratch: замени тело на реализацию.
{{.Signature}} {
	{{.Body}}
}
`))

type handlerData struct {
	Svc       Service
	RPC       RPC
	Module    string
	Signature string
	Body      string
}

// renderServer собирает server.go для сервиса.
func renderServer(svc Service, module string) ([]byte, error) {
	return render(serverTemplate, handlerData{Svc: svc, Module: module})
}

// renderHandler собирает файл одной ручки.
func renderHandler(svc Service, rpc RPC, module string) ([]byte, error) {
	d := handlerData{
		Svc:       svc,
		RPC:       rpc,
		Module:    module,
		Signature: signature(svc, rpc),
		Body:      body(rpc),
	}
	return render(handlerTemplate, d)
}

func render(t *template.Template, d handlerData) ([]byte, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return nil, fmt.Errorf("рендер шаблона: %w", err)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("форматирование сгенерированного кода: %w\n%s", err, buf.String())
	}
	return out, nil
}

// signature строит сигнатуру метода под нужный вид rpc.
// Имена стрим-типов совпадают с алиасами, которые генерирует
// protoc-gen-go-grpc: <Service>_<RPC>Server.
func signature(svc Service, rpc RPC) string {
	stream := fmt.Sprintf("%s.%s_%sServer", svc.Alias, svc.GoName, rpc.GoName)
	switch {
	case rpc.StreamRequest:
		// клиентский и двунаправленный стриминг: только поток.
		return fmt.Sprintf("func (s *Server) %s(stream %s) error", rpc.GoName, stream)
	case rpc.StreamResponse:
		return fmt.Sprintf("func (s *Server) %s(req *%s.%s, stream %s) error", rpc.GoName, svc.Alias, rpc.GoRequest, stream)
	default:
		return fmt.Sprintf("func (s *Server) %s(ctx context.Context, req *%s.%s) (*%s.%s, error)",
			rpc.GoName, svc.Alias, rpc.GoRequest, svc.Alias, rpc.GoResponse)
	}
}

// body — тело заготовки: явный Unimplemented, чтобы ручка честно
// отвечала до того, как её реализуют.
func body(rpc RPC) string {
	msg := fmt.Sprintf("%q", rpc.Name+" не реализован")
	if rpc.Streaming() {
		unused := "_ = stream"
		if !rpc.StreamRequest {
			// у серверного стриминга запрос — обычный аргумент.
			unused = "_, _ = req, stream"
		}
		return fmt.Sprintf("%s\n\treturn status.Error(codes.Unimplemented, %s)", unused, msg)
	}
	return fmt.Sprintf("_, _ = ctx, req\n\treturn nil, status.Error(codes.Unimplemented, %s)", msg)
}

// fileName превращает имя rpc в имя файла: ListUsers → list_users.go.
func fileName(rpcName string) string {
	return snake(rpcName) + ".go"
}

// snake переводит PascalCase в snake_case, схлопывая аббревиатуры:
// HTTPGet → http_get, ListUsersV2 → list_users_v2.
func snake(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && !unicode.IsUpper(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if i > 0 && (prevLower || nextLower) {
				b.WriteRune('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
