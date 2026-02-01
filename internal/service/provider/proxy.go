package provider

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// ProxyPool 代理池管理器
type ProxyPool struct {
	proxies []string
	mu      sync.RWMutex
	index   int
}

var (
	globalProxyPool *ProxyPool
	once           sync.Once
)

// GetProxyPool 获取全局代理池实例
func GetProxyPool() *ProxyPool {
	once.Do(func() {
		globalProxyPool = NewProxyPool()
	})
	return globalProxyPool
}

// NewProxyPool 创建新的代理池
func NewProxyPool() *ProxyPool {
	pool := &ProxyPool{
		proxies: make([]string, 0),
	}
	pool.loadProxiesFromEnv()
	return pool
}

// loadProxiesFromEnv 从环境变量加载代理列表
func (p *ProxyPool) loadProxiesFromEnv() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	proxyEnv := os.Getenv("SOCKS_PROXY_POOL")
	if proxyEnv == "" {
		return
	}
	
	// 解析逗号分隔的代理列表
	proxiesStr := strings.Split(proxyEnv, ",")
	for _, proxyStr := range proxiesStr {
		proxyStr = strings.TrimSpace(proxyStr)
		if proxyStr != "" {
			p.proxies = append(p.proxies, proxyStr)
		}
	}
}

// GetNextProxy 获取下一个代理(轮询方式)
func (p *ProxyPool) GetNextProxy() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if len(p.proxies) == 0 {
		return ""
	}
	
	proxy := p.proxies[p.index]
	p.index = (p.index + 1) % len(p.proxies)
	return proxy
}

// GetRandomProxy 获取随机代理
func (p *ProxyPool) GetRandomProxy() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	if len(p.proxies) == 0 {
		return ""
	}
	
	index := rand.Intn(len(p.proxies))
	return p.proxies[index]
}

// HasProxies 检查是否有可用代理
func (p *ProxyPool) HasProxies() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.proxies) > 0
}

// Count 返回代理数量
func (p *ProxyPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.proxies)
}

// GetAllProxies 获取所有代理列表(用于测试)
func (p *ProxyPool) GetAllProxies() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]string, len(p.proxies))
	copy(result, p.proxies)
	return result
}

// createSOCKS5Transport 创建SOCKS5代理传输层
func createSOCKS5Transport(proxyURL string, timeout time.Duration) (*http.Transport, error) {
	// 处理自定义格式：socks5://host:port:username:password
	// 转换为标准格式：socks5://username:password@host:port
	if strings.Contains(proxyURL, "socks5://") && strings.Count(proxyURL, ":") == 4 {
		// 解析自定义格式
		parts := strings.Split(proxyURL, ":")
		if len(parts) == 5 {
			// parts[0] = "socks5", parts[1] = "//host", parts[2] = "port", parts[3] = "username", parts[4] = "password"
			host := strings.TrimPrefix(parts[1], "//")
			port := parts[2]
			username := parts[3]
			password := parts[4]
			
			// 重构为标准URL格式
			proxyURL = fmt.Sprintf("socks5://%s:%s@%s:%s", username, password, host, port)
		}
	}
	
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("解析代理URL失败: %v", err)
	}
	
	if u.Scheme != "socks5" {
		return nil, fmt.Errorf("仅支持SOCKS5代理")
	}
	
	// 解析用户名和密码
	var auth *proxy.Auth
	if u.User != nil {
		password, _ := u.User.Password()
		auth = &proxy.Auth{
			User:     u.User.Username(),
			Password: password,
		}
	}
	
	// 创建SOCKS5拨号器
	dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("创建SOCKS5拨号器失败: %v", err)
	}
	
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	
	return transport, nil
}

// parseCustomProxyURL 解析自定义代理URL格式
func parseCustomProxyURL(proxyURL string) string {
	// 处理自定义格式：socks5://host:port:username:password
	// 转换为标准格式：socks5://username:password@host:port
	if strings.Contains(proxyURL, "socks5://") && strings.Count(proxyURL, ":") == 4 {
		// 解析自定义格式
		parts := strings.Split(proxyURL, ":")
		if len(parts) == 5 {
			// parts[0] = "socks5", parts[1] = "//host", parts[2] = "port", parts[3] = "username", parts[4] = "password"
			host := strings.TrimPrefix(parts[1], "//")
			port := parts[2]
			username := parts[3]
			password := parts[4]
			
			// 重构为标准URL格式
			return fmt.Sprintf("socks5://%s:%s@%s:%s", username, password, host, port)
		}
	}
	return proxyURL
}

// NewHTTPClientWithProxy 创建带指定代理的HTTP客户端
func NewHTTPClientWithProxy(proxyURL string, timeout time.Duration) (*http.Client, error) {
	if timeout == 0 {
		timeout = 600 * time.Second // 10分钟超时，支持长时间流式响应
	}
	
	if proxyURL == "" {
		// 没有代理，使用默认客户端
		return &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:        100,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
			Timeout: timeout,
		}, nil
	}
	
	// 转换自定义格式到标准格式
	standardURL := parseCustomProxyURL(proxyURL)
	
	// 解析代理URL
	u, err := url.Parse(standardURL)
	if err != nil {
		return nil, fmt.Errorf("解析代理URL失败: %v", err)
	}
	
	var transport *http.Transport
	
	if u.Scheme == "socks5" {
		// SOCKS5代理 - 使用转换后的标准URL
		transport, err = createSOCKS5Transport(standardURL, timeout)
		if err != nil {
			return nil, err
		}
	} else {
		// HTTP代理
		transport = &http.Transport{
			Proxy: http.ProxyURL(u),
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
	}
	
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}