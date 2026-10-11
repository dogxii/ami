package main

import (
	"os"
	"strings"

	buildinfo "github.com/dogxii/ami"
	"github.com/dogxii/ami/internal/ami"
)

func main() {
	os.Exit(ami.Main(os.Args[1:], strings.TrimSpace(buildinfo.Version)))
}
