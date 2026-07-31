package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nikita/scratch/internal/generator"
)

func newCmd(version string) *cobra.Command {
	var (
		name       string
		dir        string
		libReplace string
		goVersion  string
		force      bool
	)

	cmd := &cobra.Command{
		Use:   "new MODULE",
		Short: "Сгенерировать новый сервис",
		Long: `Генерирует каркас сервиса: gRPC + HTTP-gateway по proto-контрактам,
Swagger на debug-порте, OTel-трейсинг, wire-DI (prod и test контейнеры),
Makefile, docker-compose c jaeger, golangci-lint и CI.`,
		Example: `  scratch new github.com/acme/billing
  scratch new github.com/acme/billing --dir ./services/billing --name billing`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if goVersion == "" {
				goVersion = detectGoVersion()
			}

			params, err := generator.NewParams(args[0], name, libReplace, goVersion, version)
			if err != nil {
				return err
			}

			target := dir
			if target == "" {
				target = params.AppName
			}

			res, err := generator.Generate(target, params, force)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Проект %s сгенерирован в %s (%d файлов, scratch %s)\n",
				params.Module, res.Dir, len(res.Files), version)
			for _, w := range res.Warnings {
				_, _ = fmt.Fprintf(out, "  ! %s\n", w)
			}
			_, _ = fmt.Fprintf(out, `
Дальше:
  cd %s
  make bootstrap   # инструменты (buf, wire, mockery, линтер) в ./bin
  make generate    # кодогенерация: proto, gateway, swagger, моки, wire
  make tidy        # зависимости
  make test build  # тесты и бинарник
  make up          # docker-compose: приложение + jaeger

Swagger:  http://localhost:8081/swagger/ (запросы Try it out идут на HTTP-порт)
Jaeger:   http://localhost:16686
`, res.Dir)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "имя приложения (по умолчанию — последний сегмент module path)")
	cmd.Flags().StringVar(&dir, "dir", "", "целевая директория (по умолчанию ./<имя>)")
	cmd.Flags().StringVar(&libReplace, "lib-replace", "", "путь для replace платформенной либы (локальная разработка)")
	cmd.Flags().StringVar(&goVersion, "go", "", "версия Go для go.mod и Dockerfile (по умолчанию — версия из go env)")
	cmd.Flags().BoolVar(&force, "force", false, "генерировать в непустую директорию")

	return cmd
}

// detectGoVersion берёт версию Go с машины разработчика: go1.24.5 → 1.24.
func detectGoVersion() string {
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		return ""
	}
	v := strings.TrimPrefix(strings.TrimSpace(string(out)), "go")
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}
