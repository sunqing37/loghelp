package main

import (
	"log"
	"loghelp/internal/diagnostics"
	"loghelp/internal/ui"
)

func main() {
	stopDiagnostics := diagnostics.StartMemory()
	defer stopDiagnostics()
	if err := ui.Run(); err != nil {
		log.Fatal(err)
	}
}
