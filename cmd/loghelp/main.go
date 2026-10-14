package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"loghelp"
	"loghelp/internal/core"
	"loghelp/internal/diagnostics"
	"loghelp/internal/ui"
	"os"
)

func main() {
	if handled, err := handleMirrorCheck(os.Args[1:]); handled {
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if handled, err := handleReleaseQuery(os.Args[1:]); handled {
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
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

// Release queries are read-only diagnostics for manual release verification.
func handleReleaseQuery(args []string) (bool, error) {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Printf("LogHelp %s r%d\n", loghelp.Version, loghelp.Revision)
		return true, nil
	}
	if len(args) == 0 || args[0] != "--check-update" {
		return false, nil
	}
	flags := flag.NewFlagSet("--check-update", flag.ContinueOnError)
	current := flags.String("current-version", loghelp.Version, "当前产品版本（仅用于只读检查）")
	revision := flags.Uint64("current-revision", loghelp.Revision, "当前修订号（仅用于只读检查）")
	if err := flags.Parse(args[1:]); err != nil {
		return true, err
	}
	if flags.NArg() != 0 {
		return true, fmt.Errorf("无法识别的检查更新参数")
	}
	update, err := core.CheckRelease(context.Background(), *current, *revision)
	if err != nil {
		return true, err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return true, encoder.Encode(struct {
		CurrentVersion  string             `json:"current_version"`
		CurrentRevision uint64             `json:"current_revision"`
		Release         core.ReleaseUpdate `json:"release"`
	}{*current, *revision, update})
}
