package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// RestartRequest starts a waiting successor before the UI exits, so launch
// failures leave the current application usable. No shell or PID polling.
type RestartRequest struct {
	mu      sync.Mutex
	child   *exec.Cmd
	control *os.File
	done    chan error
}

func (r *RestartRequest) Prepare(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.child != nil {
		return fmt.Errorf("重启已准备，请稍候")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位当前程序：%w", err)
	}
	input, control, err := os.Pipe()
	if err != nil {
		return err
	}
	defer input.Close()
	status, output, err := os.Pipe()
	if err != nil {
		control.Close()
		return err
	}
	defer status.Close()
	defer output.Close()
	command := exec.Command(exe, "--wait-restart")
	command.Env = updateEnvironment()
	command.Stdin, command.Stdout, command.Stderr = input, output, os.Stderr
	configureProcess(command)
	if err := command.Start(); err != nil {
		control.Close()
		return fmt.Errorf("启动重启进程失败：%w", err)
	}
	input.Close()
	output.Close()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(io.LimitReader(status, 32)).ReadString('\n')
		if err == nil && line != "ready\n" {
			err = fmt.Errorf("重启进程返回无效响应")
		}
		ready <- err
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case err = <-ready:
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
		err = fmt.Errorf("重启进程准备超时")
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		control.Close()
		_ = command.Process.Kill()
		<-done
		return fmt.Errorf("准备重启失败：%w", err)
	}
	r.child, r.control, r.done = command, control, done
	return nil
}

// Complete is called after UI and device cleanup, before flushing diagnostics
// and releasing the instance lock. The successor waits for that lock itself.
func (r *RestartRequest) Complete(appErr error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.child == nil {
		return appErr
	}
	defer r.control.Close()
	if appErr != nil {
		_ = r.child.Process.Kill()
		<-r.done
		return appErr
	}
	select {
	case err := <-r.done:
		return fmt.Errorf("重启进程提前退出：%v", err)
	default:
	}
	if _, err := io.WriteString(r.control, "start\n"); err != nil {
		_ = r.child.Process.Kill()
		<-r.done
		return fmt.Errorf("通知新进程启动失败：%w", err)
	}
	return nil
}

// AwaitRestart is only used by a successor launched with explicit pipe handles.
// EOF, cancellation or timeout never causes a surprise application launch.
func AwaitRestart(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "--wait-restart" {
		return false, nil
	}
	if len(args) != 1 {
		return true, fmt.Errorf("无效的重启参数")
	}
	if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
		return true, err
	}
	result := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(io.LimitReader(os.Stdin, 32)).ReadString('\n')
		if err == nil && line != "start\n" {
			err = fmt.Errorf("无效的重启通知")
		}
		result <- err
	}()
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	select {
	case err := <-result:
		return true, err
	case <-timer.C:
		return true, fmt.Errorf("等待重启通知超时")
	}
}
