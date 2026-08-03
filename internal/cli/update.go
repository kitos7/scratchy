package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/kitos7/scratchy/internal/generator"
)

func updateCmd(version string) *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Обновить сгенерированный проект до текущей версии scratch",
		Long: `Обновляет managed-файлы тулинга (Makefile, buf.*, линтер, CI, Dockerfile,
docker-compose) до шаблонов текущей версии scratch и поднимает версию
платформенной либы в go.mod.

Файлы, изменённые руками, не перезаписываются: новая версия кладётся рядом
с суффиксом .scratch-new. Код приложения (cmd/, internal/, api/) не трогается.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := generator.Update(dir, version)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Обновление: %s → %s\n\n", report.FromVersion, report.ToVersion)

			paths := make([]string, 0, len(report.Files))
			for p := range report.Files {
				paths = append(paths, p)
			}
			sort.Strings(paths)

			conflicts := 0
			for _, p := range paths {
				status := report.Files[p]
				if status == generator.StatusConflict {
					conflicts++
				}
				_, _ = fmt.Fprintf(out, "  %-10s %s\n", status, p)
			}

			if report.LibBumped {
				_, _ = fmt.Fprintf(out, "\ngo.mod: %s → %s\n", generator.ScratchModule, report.ToVersion)
			}
			for _, w := range report.Warnings {
				_, _ = fmt.Fprintf(out, "  ! %s\n", w)
			}
			if conflicts > 0 {
				_, _ = fmt.Fprintf(out, "\n%d файл(ов) менялись руками — смотри *.scratch-new и перенеси изменения вручную.\n", conflicts)
			}
			_, _ = fmt.Fprintln(out, "\nДальше: make tidy generate test")
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", ".", "директория проекта")
	return cmd
}
