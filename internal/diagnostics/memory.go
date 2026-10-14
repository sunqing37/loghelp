// Package diagnostics records opt-in local profiles without opening a network port.
package diagnostics

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

type memorySample struct {
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	HeapAlloc      uint64  `json:"heap_alloc"`
	HeapInuse      uint64  `json:"heap_inuse"`
	HeapSys        uint64  `json:"heap_sys"`
	HeapReleased   uint64  `json:"heap_released"`
	Sys            uint64  `json:"sys"`
	TotalAlloc     uint64  `json:"total_alloc"`
	HeapObjects    uint64  `json:"heap_objects"`
	NumGC          uint32  `json:"num_gc"`
	Goroutines     int     `json:"goroutines"`
	GPUImageBytes  int64   `json:"gpu_image_bytes"`
	Graphics       string  `json:"graphics_library"`
	ForcedGC       bool    `json:"forced_gc"`
	UI             uiState `json:"ui"`
	MaxInputWidth  int64   `json:"max_input_width"`
	MaxInputHeight int64   `json:"max_input_height"`
	Focused        bool    `json:"focused"`
}

type uiState struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Scene  string `json:"scene"`
}

var memoryEnabled atomic.Bool
var currentUI atomic.Pointer[uiState]
var maxInputWidth, maxInputHeight atomic.Int64

func RecordUI(width, height int, scene string) {
	if memoryEnabled.Load() {
		currentUI.Store(&uiState{width, height, scene})
	}
}

func RecordInput(width, height int) {
	if !memoryEnabled.Load() {
		return
	}
	for old := maxInputWidth.Load(); int64(width) > old; old = maxInputWidth.Load() {
		if maxInputWidth.CompareAndSwap(old, int64(width)) {
			break
		}
	}
	for old := maxInputHeight.Load(); int64(height) > old; old = maxInputHeight.Load() {
		if maxInputHeight.CompareAndSwap(old, int64(height)) {
			break
		}
	}
}

// StartMemory records samples every five seconds and post-GC heap profiles every
// thirty seconds (up to ten minutes) when LOGHELP_PROFILE_DIR is set. Profiling
// defaults to forced GC at checkpoints; LOGHELP_PROFILE_GC=0 leaves GC natural.
// Normal application runs are unaffected.
func StartMemory() func() {
	dir := os.Getenv("LOGHELP_PROFILE_DIR")
	if dir == "" {
		return func() {}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("memory diagnostics: %v", err)
		return func() {}
	}
	file, err := os.Create(filepath.Join(dir, "memory.jsonl"))
	if err != nil {
		log.Printf("memory diagnostics: %v", err)
		return func() {}
	}
	started := time.Now()
	memoryEnabled.Store(true)
	forceGC := os.Getenv("LOGHELP_PROFILE_GC") != "0"
	encoder := json.NewEncoder(file)
	sample := func() {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		var graphics ebiten.DebugInfo
		ebiten.ReadDebugInfo(&graphics)
		state := uiState{}
		if value := currentUI.Load(); value != nil {
			state = *value
		}
		if err := encoder.Encode(memorySample{
			ElapsedSeconds: time.Since(started).Seconds(), HeapAlloc: m.HeapAlloc, HeapInuse: m.HeapInuse,
			HeapSys: m.HeapSys, HeapReleased: m.HeapReleased, Sys: m.Sys, TotalAlloc: m.TotalAlloc,
			HeapObjects: m.HeapObjects, NumGC: m.NumGC, Goroutines: runtime.NumGoroutine(),
			GPUImageBytes: graphics.TotalGPUImageMemoryUsageInBytes, Graphics: graphics.GraphicsLibrary.String(), ForcedGC: forceGC,
			UI: state, MaxInputWidth: maxInputWidth.Load(), MaxInputHeight: maxInputHeight.Load(),
			Focused: ebiten.IsFocused(),
		}); err != nil {
			log.Printf("memory diagnostics: %v", err)
		}
	}
	profile := func() {
		if forceGC {
			runtime.GC()
		}
		path := filepath.Join(dir, fmt.Sprintf("heap-%03d.pprof", int(time.Since(started).Seconds())))
		out, err := os.Create(path)
		if err != nil {
			log.Printf("memory diagnostics: %v", err)
			return
		}
		err = pprof.WriteHeapProfile(out)
		closeErr := out.Close()
		if err != nil || closeErr != nil {
			log.Printf("memory diagnostics: write=%v close=%v", err, closeErr)
		}
	}
	profile()
	sample()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		defer file.Close()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for tick := 1; ; tick++ {
			select {
			case <-stop:
				profile()
				sample()
				return
			case <-ticker.C:
				if tick%6 == 0 && tick <= 120 {
					profile()
				}
				sample()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop); <-done }) }
}
