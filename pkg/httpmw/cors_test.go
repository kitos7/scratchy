package httpmw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kitos7/scratchy/pkg/config"
	"github.com/kitos7/scratchy/pkg/httpmw"
)

const allowedOrigin = "https://app.example.com"

// serve прогоняет запрос через CORS-middleware. Вложенный хендлер помечает
// себя заголовком, чтобы было видно, дошёл ли до него запрос.
func serve(t *testing.T, cfg config.CORS, r *http.Request) *http.Response {
	t.Helper()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Reached-Handler", "1")
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	httpmw.CORS(cfg)(next).ServeHTTP(rec, r)
	return rec.Result()
}

func preflight(origin string) *http.Request {
	r := httptest.NewRequest(http.MethodOptions, "/v1/echo", nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type,authorization")
	return r
}

func simple(origin string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/echo", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func testConfig() config.CORS {
	return config.CORS{
		AllowedOrigins: []string{allowedOrigin},
		MaxAge:         10 * time.Minute,
	}
}

// Главное: preflight не должен доходить до gateway — тот отвечает на OPTIONS
// 501 Not Implemented, и браузер блокирует запрос.
func TestCORS_PreflightAnsweredWithoutReachingHandler(t *testing.T) {
	resp := serve(t, testConfig(), preflight(allowedOrigin))

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("код = %d, want 204", resp.StatusCode)
	}
	if resp.Header.Get("X-Reached-Handler") != "" {
		t.Error("preflight дошёл до gateway — тот ответит 501")
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != allowedOrigin {
		t.Errorf("Allow-Origin = %q, want %q", got, allowedOrigin)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); got != "POST" {
		t.Errorf("Allow-Methods = %q, want POST", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); got != "content-type,authorization" {
		t.Errorf("Allow-Headers = %q — запрошенные заголовки не разрешены", got)
	}
	if got := resp.Header.Get("Access-Control-Max-Age"); got != "600" {
		t.Errorf("Max-Age = %q, want 600", got)
	}
}

func TestCORS_SimpleRequestGetsHeaders(t *testing.T) {
	resp := serve(t, testConfig(), simple(allowedOrigin))

	if resp.Header.Get("X-Reached-Handler") != "1" {
		t.Error("обычный запрос не дошёл до gateway")
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != allowedOrigin {
		t.Errorf("Allow-Origin = %q — браузер не даст JS прочитать ответ", got)
	}
}

// Vary обязателен: без него кеш отдаст ответ с чужим разрешением.
func TestCORS_VaryOnOrigin(t *testing.T) {
	for _, c := range []struct {
		name string
		req  *http.Request
	}{
		{"обычный запрос", simple(allowedOrigin)},
		{"без Origin", simple("")},
		{"preflight", preflight(allowedOrigin)},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp := serve(t, testConfig(), c.req)
			if vary := resp.Header.Values("Vary"); len(vary) == 0 || vary[0] != "Origin" {
				t.Errorf("Vary = %v, ожидался Origin", vary)
			}
		})
	}
}

func TestCORS_ForeignOriginDenied(t *testing.T) {
	t.Run("обычный запрос проходит, но без разрешения", func(t *testing.T) {
		resp := serve(t, testConfig(), simple("https://evil.example"))

		if resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Error("чужому origin выдано разрешение")
		}
		// Сам запрос не блокируем — это делает браузер.
		if resp.Header.Get("X-Reached-Handler") != "1" {
			t.Error("обычный запрос не должен обрываться в middleware")
		}
	})

	t.Run("preflight — 204 без разрешения", func(t *testing.T) {
		resp := serve(t, testConfig(), preflight("https://evil.example"))

		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("код = %d, want 204", resp.StatusCode)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Error("чужому origin выдано разрешение")
		}
		if resp.Header.Get("Access-Control-Allow-Methods") != "" {
			t.Error("чужому origin перечислены разрешённые методы")
		}
	})
}

// Запрос без Origin — не кросс-доменный: заголовков быть не должно.
func TestCORS_NoOriginNoHeaders(t *testing.T) {
	resp := serve(t, testConfig(), simple(""))

	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("заголовок выставлен запросу без Origin")
	}
	if resp.Header.Get("X-Reached-Handler") != "1" {
		t.Error("запрос не дошёл до gateway")
	}
}

// Обычный OPTIONS без Access-Control-Request-Method — не preflight,
// его нельзя перехватывать.
func TestCORS_PlainOptionsPassesThrough(t *testing.T) {
	r := httptest.NewRequest(http.MethodOptions, "/v1/echo", nil)
	r.Header.Set("Origin", allowedOrigin)

	resp := serve(t, testConfig(), r)

	if resp.Header.Get("X-Reached-Handler") != "1" {
		t.Error("обычный OPTIONS перехвачен как preflight")
	}
}

func TestCORS_Wildcard(t *testing.T) {
	cfg := config.CORS{AllowedOrigins: []string{"*"}, MaxAge: time.Minute}

	resp := serve(t, cfg, simple("https://любой.example"))

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
}

// «*» вместе с Allow-Credentials браузер отвергает, поэтому признак
// учётных данных при "*" выставляться не должен.
func TestCORS_WildcardSuppressesCredentials(t *testing.T) {
	cfg := config.CORS{AllowedOrigins: []string{"*"}, AllowCredentials: true, MaxAge: time.Minute}

	resp := serve(t, cfg, simple("https://любой.example"))

	if resp.Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Error("с * выставлен Allow-Credentials — браузер отвергнет такой ответ")
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
}

func TestCORS_CredentialsWithExplicitOrigin(t *testing.T) {
	cfg := testConfig()
	cfg.AllowCredentials = true

	resp := serve(t, cfg, simple(allowedOrigin))

	if resp.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("Allow-Credentials не выставлен при явном списке origin")
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != allowedOrigin {
		t.Errorf("Allow-Origin = %q, ожидался конкретный origin, а не *", got)
	}
}

func TestCORSEnabled(t *testing.T) {
	if (config.CORS{}).Enabled() {
		t.Error("пустой список origin должен считаться выключенным CORS")
	}
	if !(config.CORS{AllowedOrigins: []string{allowedOrigin}}).Enabled() {
		t.Error("непустой список origin должен включать CORS")
	}
}
