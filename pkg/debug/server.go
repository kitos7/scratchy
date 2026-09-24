// Package debug — HTTP-сервер технических ручек на debug-порте:
// Swagger UI, /metrics (Prometheus), /healthz, /readyz, pprof.
package debug

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Options — параметры debug-сервера.
type Options struct {
	// SwaggerJSON — сгенерированная OpenAPI-спецификация; nil отключает Swagger.
	SwaggerJSON []byte
	// Title — название сервиса для спецификации.
	Title string
	// TargetHost — host[:port], куда Swagger UI шлёт запросы Try it out.
	TargetHost string
	// Handlers — дополнительные хендлеры на debug-порте (pattern → handler),
	// например встраиваемые админки. Без авторизации: debug-порт наружу
	// не публикуется.
	Handlers map[string]http.Handler
}

// Server — debug-сервер.
type Server struct {
	server *http.Server
	ready  atomic.Bool
}

// New собирает debug-сервер на указанном порте.
func New(port int, opts Options) *Server {
	s := &Server{}
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !s.ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.Handle("/metrics", promhttp.Handler())

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	for pattern, h := range opts.Handlers {
		mux.Handle(pattern, h)
	}

	if opts.SwaggerJSON != nil {
		spec := patchSwagger(opts.SwaggerJSON, opts.TargetHost, opts.Title)
		mux.HandleFunc("/swagger.json", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(spec)
		})
		mux.HandleFunc("/swagger/", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(swaggerHTML))
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.Redirect(w, r, "/swagger/", http.StatusFound)
				return
			}
			http.NotFound(w, r)
		})
	}

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// SetReady переключает статус /readyz.
func (s *Server) SetReady(v bool) { s.ready.Store(v) }

// Start блокирующе запускает сервер.
func (s *Server) Start() error { return s.server.ListenAndServe() }

// Shutdown мягко останавливает сервер.
func (s *Server) Shutdown(ctx context.Context) error { return s.server.Shutdown(ctx) }
