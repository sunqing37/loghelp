package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const LatestReleaseURL = releaseRepository + "latest"
const releaseCheckAttempts = 3

// An empty proxy preserves the default environment settings; direct bypasses
// them. An explicit URL is shared by release queries and package downloads.
type UpdateNetworkOptions struct {
	Proxy   string
	Attempt func(attempt, total int)
}

func ValidateUpdateProxy(value string) error {
	_, err := parseUpdateProxy(value)
	return err
}

func parseUpdateProxy(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "direct" {
		return nil, nil
	}
	proxy, err := url.Parse(value)
	if err != nil || proxy.Hostname() == "" || proxy.Opaque != "" || proxy.RawQuery != "" || proxy.Fragment != "" || proxy.Path != "" && proxy.Path != "/" {
		return nil, fmt.Errorf("代理地址无效，请填写完整地址，例如 http://127.0.0.1:7890")
	}
	switch proxy.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("代理协议需使用 http、https、socks5 或 socks5h")
	}
	if strings.HasSuffix(proxy.Host, ":") {
		return nil, fmt.Errorf("代理端口不能为空")
	}
	if port := proxy.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, fmt.Errorf("代理端口需为 1–65535")
		}
	}
	return proxy, nil
}

type updateRequestError struct {
	message string
	retry   bool
	cause   error
}

func (err *updateRequestError) Error() string { return err.message }
func (err *updateRequestError) Unwrap() error { return err.cause }

func newUpdateClient(proxyValue string, timeout time.Duration) (*http.Client, error) {
	proxy, err := parseUpdateProxy(proxyValue)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	switch strings.TrimSpace(proxyValue) {
	case "":
		transport.Proxy = func(request *http.Request) (*url.URL, error) {
			proxy, err := http.ProxyFromEnvironment(request)
			if err == nil && proxy != nil {
				err = ValidateUpdateProxy(proxy.String())
			}
			if err != nil {
				return nil, &updateRequestError{message: "环境变量中的代理地址无效，请检查 HTTPS_PROXY、HTTP_PROXY，或改用自定义代理", cause: err}
			}
			return proxy, nil
		}
	case "direct":
		transport.Proxy = nil
	default:
		transport.Proxy = http.ProxyURL(proxy)
	}
	transport.OnProxyConnectResponse = func(_ context.Context, _ *url.URL, _ *http.Request, response *http.Response) error {
		if response.StatusCode == http.StatusOK {
			return nil
		}
		message := fmt.Sprintf("代理服务器拒绝连接（HTTP %d），请检查代理规则和节点", response.StatusCode)
		if response.StatusCode == http.StatusProxyAuthRequired {
			message = "代理认证失败（HTTP 407），请检查代理用户名和密码"
		}
		return &updateRequestError{message: message, retry: retryUpdateStatus(response.StatusCode)}
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

func retryUpdateStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusInternalServerError ||
		status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// Avoid printing raw transport errors: proxy URLs can contain credentials.
func updateConnectionError(err error) (error, bool) {
	var requestErr *updateRequestError
	if errors.As(err, &requestErr) {
		return requestErr, requestErr.retry
	}
	message, retry := "连接 GitHub 失败，请检查网络、代理地址、端口和分流规则", true
	var dns *net.DNSError
	var network *net.OpError
	var timeout net.Error
	var unknownAuthority x509.UnknownAuthorityError
	var invalidCertificate x509.CertificateInvalidError
	var hostname x509.HostnameError
	var tlsHeader tls.RecordHeaderError
	switch {
	case errors.Is(err, context.Canceled):
		message, retry = "更新请求已取消", false
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		message = "连接或读取 GitHub 超时，请检查网络及代理是否可用"
	case errors.As(err, &unknownAuthority), errors.As(err, &invalidCertificate), errors.As(err, &hostname):
		message, retry = "HTTPS 证书校验失败，请检查系统时间、证书及代理的 HTTPS 设置", false
	case errors.As(err, &tlsHeader):
		message, retry = "TLS 握手失败，请检查代理协议是否与端口匹配", false
	case errors.As(err, &dns):
		message = "域名解析失败，请检查 DNS 和代理地址"
	case errors.As(err, &network):
		if network.Op == "proxyconnect" {
			message = "无法连接代理服务器，请检查代理是否启动及地址、端口"
		} else {
			message = "网络连接失败或中断，请检查代理节点及分流规则"
		}
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		message = "GitHub 连接提前中断，请检查网络及代理节点"
	}
	return &updateRequestError{message: message, retry: retry, cause: err}, retry
}

func releaseHTTPError(response *http.Response, body []byte) (error, bool) {
	status := response.StatusCode
	message := fmt.Sprintf("GitHub 返回 HTTP %d，请稍后重试", status)
	switch status {
	case http.StatusNotFound:
		message = "GitHub 未找到正式发布版本（HTTP 404）"
	case http.StatusProxyAuthRequired:
		message = "代理认证失败（HTTP 407），请检查代理用户名和密码"
	case http.StatusForbidden, http.StatusTooManyRequests:
		limited := status == http.StatusTooManyRequests || response.Header.Get("X-RateLimit-Remaining") == "0" || strings.Contains(strings.ToLower(string(body)), "rate limit")
		if limited {
			message = fmt.Sprintf("GitHub 请求限流（HTTP %d），共享代理出口也可能耗尽额度，请稍后重试", status)
			if seconds, err := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 64); err == nil && seconds > 0 {
				message += fmt.Sprintf("；建议等待 %d 秒", seconds)
			} else if reset, err := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
				message += "；额度恢复时间：" + time.Unix(reset, 0).Local().Format("01-02 15:04:05 MST")
			}
		} else {
			message = "GitHub 访问被拒绝（HTTP 403），请检查代理规则或网络拦截"
		}
	case http.StatusUnauthorized:
		message = "GitHub 返回认证错误（HTTP 401），请检查网络或代理认证配置"
	}
	return &updateRequestError{message: message, retry: retryUpdateStatus(status)}, retryUpdateStatus(status)
}

func fetchRelease(ctx context.Context, client *http.Client, current string, revision uint64) ([]byte, error, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/sunqing37/loghelp/releases/latest", nil)
	if err != nil {
		return nil, err, false
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", fmt.Sprintf("LogHelp/%s-r%d", current, revision))
	response, err := client.Do(request)
	if err != nil {
		err, retry := updateConnectionError(err)
		return nil, err, retry
	}
	defer response.Body.Close()
	const maxResponse = 2 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if response.StatusCode != http.StatusOK {
		statusErr, retry := releaseHTTPError(response, data)
		return nil, statusErr, retry
	}
	if err != nil {
		err, retry := updateConnectionError(err)
		return nil, fmt.Errorf("读取版本信息失败：%w", err), retry
	}
	if len(data) > maxResponse {
		return nil, fmt.Errorf("GitHub 版本信息过大"), false
	}
	return data, nil, false
}

func fetchReleaseWithRetry(ctx context.Context, current string, revision uint64, options UpdateNetworkOptions) ([]byte, error) {
	client, err := newUpdateClient(options.Proxy, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	for attempt := 1; attempt <= releaseCheckAttempts; attempt++ {
		if ctx.Err() != nil {
			err, _ := updateConnectionError(ctx.Err())
			return nil, err
		}
		if options.Attempt != nil {
			options.Attempt(attempt, releaseCheckAttempts)
		}
		data, err, retry := fetchRelease(ctx, client, current, revision)
		if err == nil {
			return data, nil
		}
		if !retry || attempt == releaseCheckAttempts {
			if attempt > 1 {
				err = fmt.Errorf("已尝试 %d 次：%w", attempt, err)
			}
			return nil, err
		}
		timer := time.NewTimer(time.Duration(attempt) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			err, _ := updateConnectionError(ctx.Err())
			return nil, err
		case <-timer.C:
		}
	}
	panic("unreachable")
}
