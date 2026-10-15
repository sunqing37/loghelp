package core

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AppInstance owns an OS lock, not a PID-file existence check. The OS releases
// the lock even on a crash; the persistent file can safely be reused next time.
type AppInstance struct {
	file        *os.File
	listener    net.Listener
	activations chan struct{}
	done        chan struct{}
	once        sync.Once
	closeErr    error
}

type instanceAddress struct {
	PID   int    `json:"pid"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// StartAppInstance returns nil after notifying an existing GUI instance. CLI
// queries and the update helper call their handlers before acquiring this lock.
func StartAppInstance() (*AppInstance, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("定位应用实例目录失败：%w", err)
	}
	directory := filepath.Join(cache, "loghelp")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("创建应用实例目录失败：%w", err)
	}
	file, err := os.OpenFile(filepath.Join(directory, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开应用实例锁失败：%w", err)
	}
	owned, err := lockAppInstance(file)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("锁定应用实例失败：%w", err)
	}
	if !owned {
		defer file.Close()
		if err := notifyAppInstance(file); err != nil {
			return nil, fmt.Errorf("LogHelp 已在运行，未能通知现有窗口，请切换到已有窗口：%w", err)
		}
		return nil, nil
	}
	fail := func(err error) (*AppInstance, error) { file.Close(); return nil, err }
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fail(fmt.Errorf("启动应用窗口通知服务失败：%w", err))
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		listener.Close()
		return fail(fmt.Errorf("生成应用窗口通知凭据失败：%w", err))
	}
	address := instanceAddress{PID: os.Getpid(), Port: listener.Addr().(*net.TCPAddr).Port, Token: hex.EncodeToString(token)}
	data, err := json.Marshal(address)
	if err != nil {
		listener.Close()
		return fail(fmt.Errorf("编码应用窗口通知地址失败：%w", err))
	}
	// Windows locks byte zero. Keep the rendezvous data after it so secondary
	// processes can read it without touching the exclusively locked byte.
	if err = file.Truncate(int64(1 + len(data))); err == nil {
		_, err = file.WriteAt(data, 1)
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		listener.Close()
		return fail(fmt.Errorf("保存应用窗口通知地址失败：%w", err))
	}
	instance := &AppInstance{file: file, listener: listener, activations: make(chan struct{}, 1), done: make(chan struct{})}
	go instance.serve(address.Token)
	return instance, nil
}

func notifyAppInstance(file *os.File) error {
	deadline := time.Now().Add(2 * time.Second)
	lastErr := fmt.Errorf("窗口通知超时")
	for time.Now().Before(deadline) {
		var address instanceAddress
		err := json.NewDecoder(io.NewSectionReader(file, 1, 1024)).Decode(&address)
		if err == nil && address.PID > 0 && address.Port > 0 && address.Port <= 65535 && len(address.Token) == 64 {
			dialer := net.Dialer{Timeout: 300 * time.Millisecond, Deadline: deadline}
			connection, dialErr := dialer.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(address.Port)))
			if dialErr == nil {
				connection.SetDeadline(deadline)
				_, err = fmt.Fprintf(connection, "activate %s\n", address.Token)
				if err == nil {
					var response string
					response, err = bufio.NewReader(io.LimitReader(connection, 16)).ReadString('\n')
					if err == nil && response != "ok\n" {
						err = fmt.Errorf("窗口通知返回无效响应")
					}
				}
				connection.Close()
				if err == nil {
					return nil
				}
			} else {
				err = dialErr
			}
		} else if err == nil {
			err = fmt.Errorf("现有实例正在初始化通知地址")
		}
		lastErr = err
		time.Sleep(min(50*time.Millisecond, max(0, time.Until(deadline))))
	}
	return lastErr
}

func (instance *AppInstance) serve(token string) {
	defer close(instance.done)
	for {
		connection, err := instance.listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("接收应用窗口通知失败：%v", err)
			}
			return
		}
		connection.SetDeadline(time.Now().Add(time.Second))
		message, err := bufio.NewReader(io.LimitReader(connection, 128)).ReadString('\n')
		if err == nil && subtle.ConstantTimeCompare([]byte(strings.TrimSuffix(message, "\n")), []byte("activate "+token)) == 1 {
			select {
			case instance.activations <- struct{}{}:
			default:
			}
			if _, err := io.WriteString(connection, "ok\n"); err != nil {
				log.Printf("确认应用窗口通知失败：%v", err)
			}
		}
		connection.Close()
	}
}

func (instance *AppInstance) Activations() <-chan struct{} { return instance.activations }

func (instance *AppInstance) Close() error {
	instance.once.Do(func() {
		listenerErr := instance.listener.Close()
		<-instance.done
		// Do not unlink the lock file: removing a locked inode permits a new
		// file/lock to be created while a third process still holds the old one.
		instance.closeErr = errors.Join(listenerErr, instance.file.Close())
	})
	return instance.closeErr
}
