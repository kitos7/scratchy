package debug

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serve поднимает хендлеры debug-сервера на случайном порту httptest,
// не занимая реальный DEBUG_PORT.
func serve(t *testing.T, opts Options) (*Server, *httptest.Server) {
	t.Helper()

	s := New(0, opts)
	ts := httptest.NewServer(s.server.Handler)
	t.Cleanup(ts.Close)
	return s, ts
}

// get возвращает код ответа и тело.
func get(t *testing.T, ts *httptest.Server, path string) (int, string) {
	t.Helper()

	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("чтение тела %s: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

func TestHealthz(t *testing.T) {
	_, ts := serve(t, Options{})

	code, body := get(t, ts, "/healthz")
	if code != http.StatusOK {
		t.Errorf("код = %d, want 200", code)
	}
	if body != "ok" {
		t.Errorf("тело = %q, want ok", body)
	}
}

// /readyz — то, по чему оркестратор снимает трафик: до SetReady(true) и
// после начала остановки он обязан отвечать 503.
func TestReadyz(t *testing.T) {
	s, ts := serve(t, Options{})

	if code, _ := get(t, ts, "/readyz"); code != http.StatusServiceUnavailable {
		t.Errorf("до SetReady код = %d, want 503", code)
	}

	s.SetReady(true)
	if code, body := get(t, ts, "/readyz"); code != http.StatusOK || body != "ok" {
		t.Errorf("после SetReady(true): код = %d, тело = %q", code, body)
	}

	s.SetReady(false)
	if code, _ := get(t, ts, "/readyz"); code != http.StatusServiceUnavailable {
		t.Errorf("после SetReady(false) код = %d, want 503", code)
	}
}

// /healthz не зависит от readiness: процесс жив, даже когда уходит из ротации.
func TestHealthzIgnoresReadiness(t *testing.T) {
	s, ts := serve(t, Options{})
	s.SetReady(false)

	if code, _ := get(t, ts, "/healthz"); code != http.StatusOK {
		t.Errorf("код = %d, want 200", code)
	}
}

func TestMetrics(t *testing.T) {
	_, ts := serve(t, Options{})

	code, body := get(t, ts, "/metrics")
	if code != http.StatusOK {
		t.Fatalf("код = %d, want 200", code)
	}
	if !strings.Contains(body, "go_goroutines") {
		t.Errorf("нет метрик Go-рантайма в ответе:\n%s", truncate(body))
	}
}

func TestPprof(t *testing.T) {
	_, ts := serve(t, Options{})

	for _, path := range []string{"/debug/pprof/", "/debug/pprof/cmdline"} {
		if code, _ := get(t, ts, path); code != http.StatusOK {
			t.Errorf("%s: код = %d, want 200", path, code)
		}
	}
}

func TestSwaggerRoutes(t *testing.T) {
	spec := []byte(`{"swagger":"2.0","host":"нужно-подменить","info":{"title":"нужно-подменить"},"paths":{}}`)
	_, ts := serve(t, Options{SwaggerJSON: spec, Title: "billing", TargetHost: "localhost:8080"})

	t.Run("/swagger.json отдаёт пропатченную спеку", func(t *testing.T) {
		code, body := get(t, ts, "/swagger.json")
		if code != http.StatusOK {
			t.Fatalf("код = %d, want 200", code)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("не JSON (%v):\n%s", err, truncate(body))
		}
		if got["host"] != "localhost:8080" {
			t.Errorf("host = %v — Try it out пойдёт не туда", got["host"])
		}
		if got["info"].(map[string]any)["title"] != "billing" {
			t.Errorf("title = %v, want billing", got["info"])
		}
	})

	t.Run("/swagger/ отдаёт UI", func(t *testing.T) {
		code, body := get(t, ts, "/swagger/")
		if code != http.StatusOK {
			t.Fatalf("код = %d, want 200", code)
		}
		if !strings.Contains(body, "SwaggerUIBundle") {
			t.Errorf("не похоже на Swagger UI:\n%s", truncate(body))
		}
		if !strings.Contains(body, `url: "/swagger.json"`) {
			t.Errorf("UI не ссылается на спеку:\n%s", truncate(body))
		}
	})

	t.Run("корень редиректит на UI", func(t *testing.T) {
		// Клиент без автоследования, чтобы увидеть сам редирект.
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
		resp, err := client.Get(ts.URL + "/")
		if err != nil {
			t.Fatalf("GET /: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusFound {
			t.Errorf("код = %d, want 302", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/swagger/" {
			t.Errorf("Location = %q, want /swagger/", loc)
		}
	})

	t.Run("неизвестный путь — 404", func(t *testing.T) {
		if code, _ := get(t, ts, "/нет-такого"); code != http.StatusNotFound {
			t.Errorf("код = %d, want 404", code)
		}
	})
}

// Без спеки swagger-ручек быть не должно, а технические — остаются.
func TestSwaggerDisabled(t *testing.T) {
	_, ts := serve(t, Options{})

	for _, path := range []string{"/swagger/", "/swagger.json"} {
		if code, _ := get(t, ts, path); code != http.StatusNotFound {
			t.Errorf("%s: код = %d, want 404", path, code)
		}
	}
	if code, _ := get(t, ts, "/healthz"); code != http.StatusOK {
		t.Errorf("/healthz сломался без swagger: код = %d", code)
	}
}

func truncate(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
