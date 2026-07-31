# scratch

Генератор Go-сервисов и платформенная либа к нему. Один бинарник `scratch` генерирует каркас сервиса и умеет обновлять его до новых версий шаблонов.

Что получает сгенерированный сервис:

- gRPC + HTTP-gateway по proto-контрактам (buf: generate / lint / breaking);
- транспорт «пакет на proto-сервис, файл на ручку»: новые rpc и целые сервисы из proto получают файлы-заготовки на шаге `make generate`;
- Swagger UI на debug-порте, запросы «Try it out» идут на публичный HTTP-порт (в docker-compose — `:80`);
- OpenTelemetry-трассировку (OTLP → jaeger) сквозь gateway → gRPC → бизнес-код, `trace_id` в логах;
- метрики RPC на `/metrics` (OTel-инструментирование gRPC и gateway → Prometheus);
- DI через google/wire: прод-контейнер (`internal/di`) и тестовый с моками (`internal/di/ditest` + mockery);
- Makefile (bootstrap / generate / build / test / lint / up), docker-compose с jaeger, golangci-lint, GitHub Actions CI;
- debug-порт: `/swagger/`, `/metrics`, `/healthz`, `/readyz`, `/debug/pprof`.

## Устройство

Репозиторий — и генератор, и рантайм:

```
cmd/scratch/           CLI (cobra): new, update, handlers
internal/cli/          команды
internal/generator/    рендер шаблонов, манифест .scratch.yaml, update
internal/handlers/     разбор proto → файлы gRPC-ручек (пакет на сервис, файл на rpc)
internal/otelres/      OTel-ресурс: трейсы и метрики описывают себя одинаково
internal/generator/templates/project/   шаблоны каркаса (go:embed)
pkg/                   платформенная либа — её импортируют сгенерированные сервисы:
  app/                 рантайм: gRPC + gateway + debug-серверы, graceful shutdown
  config/              базовый конфиг из env (+ generic Load[T])
  logging/             slog c trace_id/span_id из контекста
  tracing/             OTel: OTLP-экспортер, сэмплинг, W3C-пропагация
  metrics/             OTel-метрики → Prometheus-registry, который отдаёт /metrics
  grpcmw/              интерсепторы: recovery и логирование, unary и stream
  debug/               debug-сервер: swagger (host → HTTP-порт), metrics, healthz, pprof
```

`app.App` расширяется опциями: `WithUnaryInterceptors` / `WithStreamInterceptors`
для gRPC, `WithGatewayOptions` (`runtime.ServeMuxOption`: маппинг заголовков,
обработчик ошибок) и `WithHTTPMiddleware` для gateway.

Обновление устроено гибридно: общий рантайм живёт в `pkg/` и обновляется через версию модуля в go.mod, а «тонкий» сгенерированный каркас почти не требует обновлений. Managed-файлы тулинга (Makefile, buf.*, .golangci.yml, .mockery.yaml, Dockerfile, docker-compose.yml, CI, .gitignore) обновляет `scratch update`: неизменённые перезаписывает, изменённые руками не трогает — кладёт новую версию рядом (`*.scratch-new`). Код приложения (`cmd/`, `internal/`, `api/`) не трогается никогда. Параметры и хэши — в `.scratch.yaml`.

## Использование

```bash
make install          # или: go install github.com/nikita/scratch/cmd/scratch@latest (после публикации)

scratch new github.com/acme/billing
cd billing
make bootstrap generate
make test build
make up               # docker-compose: сервис + jaeger
```

Пока либа не опубликована (module path — placeholder `github.com/nikita/scratch`), генерируй с replace на локальную копию: `scratch new github.com/acme/demo --lib-replace /путь/к/go-scratch`.

## Версионирование и релизы

Версия шаблонов = версия бинарника (semver-теги этого репозитория):

```bash
git tag v0.1.0 && git push --tags
```

`make build` вшивает версию через ldflags (`git describe`), при `go install ...@v0.1.0` версия берётся из buildinfo. В сгенерированном проекте `scratch update` поднимает require либы до версии бинарника и обновляет managed-файлы.

Сборка с незакоммиченными правками даёт версию с суффиксом `dirty`. Такая версия не является валидной версией модуля, поэтому scratch считает сборку нерелизной: в go.mod проекта попадает placeholder и обязателен `--lib-replace`. Для проверки релизного поведения собирай на чистом дереве.

## Разработка

```bash
make check   # gofmt + vet + go test -race -cover + линтер — гейт перед коммитом
make e2e     # сквозной прогон: сгенерированный проект собирается (нужна сеть)
```

`make check` — то, что дал бы CI; пока у репозитория нет remote, это единственный гейт. `make e2e` генерирует проект во временную директорию и проходит по нему `bootstrap → generate → test → build`: только он проверяет связку buf + wire + mockery + `scratch handlers` целиком.

Перед публикацией замени module path `github.com/nikita/scratch` на реальный (go.mod, импорты, `internal/generator/params.go: ScratchModule`).

## Разработка

```bash
make build    # bin/scratch
make test     # тесты генератора: рендер всех шаблонов, generate+update сценарий
make lint     # golangci-lint
```
