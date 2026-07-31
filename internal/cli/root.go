// Package cli — команды бинарника scratch.
package cli

import "github.com/spf13/cobra"

// Execute запускает CLI.
func Execute(version string) error {
	root := &cobra.Command{
		Use:          "scratch",
		Short:        "Генератор Go-сервисов: gRPC + gateway, Swagger, трейсинг, wire, docker-compose",
		Version:      version,
		SilenceUsage: true,
	}

	root.AddCommand(
		newCmd(version),
		updateCmd(version),
		handlersCmd(),
	)

	return root.Execute()
}
