package main

import (
	"log"
	"loghelp/internal/core"
	"loghelp/internal/diagnostics"
	"loghelp/internal/ui"
	"os"
)

func main() {
	if handled, err := core.HandleUpdateCommand(os.Args[1:]); handled {
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	stopDiagnostics := diagnostics.StartMemory()
	defer stopDiagnostics()
	if err := ui.Run(); err != nil {
		log.Fatal(err)
	}
}
