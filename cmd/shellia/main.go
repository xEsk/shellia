package main

import (
	"context"
	"os"

	"shellia/internal/app"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	app.SetVersion(version)
	if code := app.Run(context.Background(), os.Args[1:]); code != 0 {
		os.Exit(code)
	}
}
