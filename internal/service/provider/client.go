package provider

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NewHTTPClient 创建HTTP客户端
// 支持HTTP和SOCKS5代理
func NewHTTPClient(proxy string, timeout time.Duration) *http.Client {
	// 如果代理是SOCKS5格式，使用新的代理客户端创建函数
	if strings.HasPrefix(proxy, "socks5://") {
		client, err := NewHTTPClientWithProxy(proxy, timeout)
		if err != nil {
			log.Printf("创建SOCKS5代理客户端失败: %v, 使用默认客户端", err)
			client, _ := NewHTTPClientWithProxy("", timeout)
			return client
		}
		return client
	}
	
	// 原有的HTTP代理逻辑
	transport := &http.Transport{}

	if proxy != "" {
		if proxyURL, err := url.Parse(proxy); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	if timeout == 0 {
		timeout = 600 * time.Second // 10分钟超时，支持长时间流式响应
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

// NewHTTPClientWithPoolProxy 使用代理池创建HTTP客户端
func NewHTTPClientWithPoolProxy(useProxy bool, timeout time.Duration) *http.Client {
	if !useProxy {
		// 不使用代理
		client, _ := NewHTTPClientWithProxy("", timeout)
		return client
	}
	
	pool := GetProxyPool()
	if !pool.HasProxies() {
		// 没有可用代理，使用默认客户端
		client, _ := NewHTTPClientWithProxy("", timeout)
		return client
	}
	
	// 获取下一个代理
	proxyURL := pool.GetNextProxy()
	client, err := NewHTTPClientWithProxy(proxyURL, timeout)
	if err != nil {
		log.Printf("使用代理 %s 创建客户端失败: %v, 使用默认客户端", proxyURL, err)
		client, _ := NewHTTPClientWithProxy("", timeout)
		return client
	}
	
	return client
}