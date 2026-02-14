package main

import (
	"embed"
	"os"

	"github.com/civilfritz/civilsort/internal/cmd"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

func main() {
	if err := cmd.Execute(templateFS, staticFS); err != nil {
		os.Exit(1)
	}
}
