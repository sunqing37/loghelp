package core

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// os/exec serializes writes when Stdout and Stderr share the same writer. The
// callback finishes before Run returns, so no late updates can cross stages.
type installOutput struct {
	output   limitedBuffer
	pending  []byte
	latest   string
	last     time.Time
	progress func(string)
}

func (o *installOutput) Write(p []byte) (int, error) {
	_, _ = o.output.Write(p)
	for _, b := range p {
		if b == '\r' || b == '\n' {
			o.takeLine()
		} else if len(o.pending) < 4096 {
			o.pending = append(o.pending, b)
		}
	}
	if time.Since(o.last) >= 250*time.Millisecond {
		o.report()
	}
	return len(p), nil
}

func (o *installOutput) takeLine() {
	if line := strings.TrimSpace(strings.ToValidUTF8(string(o.pending), "�")); line != "" {
		o.latest = line
	}
	o.pending = o.pending[:0]
}

func (o *installOutput) report() {
	if o.latest != "" && o.progress != nil {
		o.progress(o.latest)
	}
	o.latest = ""
	o.last = time.Now()
}

func runInstallCommand(cmd *exec.Cmd, progress func(string)) (string, error) {
	out := &installOutput{output: limitedBuffer{limit: 4 << 20}, progress: progress}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	out.takeLine()
	out.report()
	value := strings.TrimSpace(out.output.b.String())
	if err != nil {
		return value, fmt.Errorf("%w：%s", err, value)
	}
	return value, nil
}
