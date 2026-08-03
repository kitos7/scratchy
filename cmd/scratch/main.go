// scratch — генератор Go-сервисов: gRPC + HTTP-gateway, Swagger,
// трассировка, wire-DI, docker-compose, линтер.
package main

import (
	"os"
	"runtime/debug"

	"github.com/kitos7/scratchy/internal/cli"
)

// version проставляется при сборке: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	if err := cli.Execute(resolveVersion()); err != nil {
		os.Exit(1)
	}
}

func resolveVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}
