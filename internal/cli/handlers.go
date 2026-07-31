package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nikita/scratch/internal/handlers"
)

func handlersCmd() *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "handlers",
		Short: "Завести файлы gRPC-ручек по proto-контрактам проекта",
		Long: `Разбирает api/proto и дописывает недостающие файлы транспорта:
пакет на proto-сервис (internal/server/<сервис>), файл на каждый rpc.

Существующие файлы не перезаписываются; ручка считается реализованной,
если метод с ресивером *Server есть где угодно в пакете сервиса.
Для нового сервиса создаётся server.go, а регистрацию в DI команда
только подсказывает — код в internal/di scratch не правит.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := handlers.Generate(dir)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, f := range report.Created {
				_, _ = fmt.Fprintf(out, "  создан    %s\n", f)
			}
			for _, w := range report.Sorted() {
				_, _ = fmt.Fprintf(out, "  ! %s\n", w)
			}
			if len(report.Created) == 0 && len(report.Warnings) == 0 {
				_, _ = fmt.Fprintf(out, "  ручки актуальны (%d реализовано)\n", len(report.Existing))
			}

			for _, svc := range report.NewServices {
				_, _ = fmt.Fprintf(out, "\nНовый сервис %s — зарегистрируй его:\n%s\n", svc.Name, handlers.DIHint(svc))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", ".", "директория проекта")
	return cmd
}
