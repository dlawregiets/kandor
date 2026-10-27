package main

import (
	"os"

	"github.com/dlawregiets/kandor/internal/app"
)

func main() {
	os.Exit(app.Main(os.Args[1:]))
}
