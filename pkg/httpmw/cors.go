// Package httpmw — middleware для HTTP-gateway.
package httpmw

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/kitos7/scratchy/pkg/config"
)

// CORS отвечает на preflight и проставляет заголовки кросс-доменного доступа.
//
// Preflight обязан заканчиваться здесь: grpc-gateway маршрутизирует только
// методы, объявленные в proto-аннотациях, и на OPTIONS отвечает
// 501 Not Implemented — то есть без этого middleware браузер не пропустит
// ни один запрос с нестандартными заголовками.
func CORS(cfg config.CORS) func(http.Handler) http.Handler {
	allowAll := slices.Contains(cfg.AllowedOrigins, "*")
	// «*» вместе с Allow-Credentials браузер отвергает, поэтому при "*"
	// признак учётных данных не выставляем вовсе.
	credentials := cfg.AllowCredentials && !allowAll
	maxAge := strconv.Itoa(int(cfg.MaxAge.Seconds()))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			// Ответ зависит от Origin: без Vary кеш (свой или CDN) отдаст
			// его другому origin вместе с чужим разрешением.
			h.Add("Vary", "Origin")

			origin := r.Header.Get("Origin")
			allowed := origin != "" && (allowAll || slices.Contains(cfg.AllowedOrigins, origin))
			if allowed {
				if allowAll && !credentials {
					h.Set("Access-Control-Allow-Origin", "*")
				} else {
					h.Set("Access-Control-Allow-Origin", origin)
				}
				if credentials {
					h.Set("Access-Control-Allow-Credentials", "true")
				}
			}

			if !isPreflight(r) {
				next.ServeHTTP(w, r)
				return
			}

			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			if allowed {
				// Отдаём именно запрошенные метод и заголовки: какие пути
				// и методы существуют, знает gateway, и на самом запросе
				// он ответит честно.
				h.Set("Access-Control-Allow-Methods", r.Header.Get("Access-Control-Request-Method"))
				if req := r.Header.Get("Access-Control-Request-Headers"); req != "" {
					h.Set("Access-Control-Allow-Headers", req)
				}
				h.Set("Access-Control-Max-Age", maxAge)
			}
			// Origin не разрешён — 204 без заголовков: запрет обеспечит
			// браузер, а 501 от gateway только запутал бы отладку.
			w.WriteHeader(http.StatusNoContent)
		})
	}
}

// isPreflight отличает предварительный запрос от обычного OPTIONS.
func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
}
