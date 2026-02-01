package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"zencoder2api/internal/service/provider"
)

// ProxyRequestOptions 代理请求选项
type ProxyRequestOptions struct {
	UseProxy         bool          // 是否使用代理
	MaxRetries       int           // 最大重试次数
	RetryDelay       time.Duration // 重试延迟
	OnError          func(error) bool // 错误判断函数，返回true表示需要重试
}

// DefaultProxyRequestOptions 默认代理请求选项
func DefaultProxyRequestOptions() ProxyRequestOptions {
	return ProxyRequestOptions{
		UseProxy:   true,
		MaxRetries: 3,
		RetryDelay: time.Second,
		OnError:    isNetworkError,
	}
}

// isNetworkError 判断是否为网络错误（可重试的错误）
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	
	errStr := err.Error()
	
	// 网络连接错误
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "connection timed out") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "dial tcp") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "EOF") {
		return true
	}
	
	// SOCKS代理相关错误
	if strings.Contains(errStr, "socks connect") ||
		strings.Contains(errStr, "proxy") {
		return true
	}
	
	return false
}

// DoRequestWithProxyRetry 执行带代理重试的HTTP请求
func DoRequestWithProxyRetry(ctx context.Context, req *http.Request, originalProxy string, options ProxyRequestOptions) (*http.Response, error) {
	// 首先尝试使用原始代理（如果有的话）
	client := provider.NewHTTPClient(originalProxy, 0)
	
	resp, err := client.Do(req)
	if err == nil {
		// 请求成功，返回结果
		return resp, nil
	}
	
	// 检查错误是否可重试
	if !options.OnError(err) {
		return nil, err
	}
	
	// 如果不使用代理池，直接返回错误
	if !options.UseProxy {
		return nil, err
	}
	
	proxyPool := provider.GetProxyPool()
	if !proxyPool.HasProxies() {
		return nil, fmt.Errorf("原始请求失败且无可用代理: %v", err)
	}
	
	var lastErr error = err
	
	// 使用代理池进行重试
	for i := 0; i < options.MaxRetries; i++ {
		// 获取下一个代理
		proxyURL := proxyPool.GetNextProxy()
		if proxyURL == "" {
			break
		}
		
		// 创建使用代理的HTTP客户端
		proxyClient, clientErr := provider.NewHTTPClientWithProxy(proxyURL, 0)
		if clientErr != nil {
			lastErr = clientErr
			time.Sleep(options.RetryDelay)
			continue
		}
		
		// 克隆请求（因为request body可能已经被消费）
		newReq := req.Clone(ctx)
		
		resp, err := proxyClient.Do(newReq)
		if err == nil {
			// 请求成功
			return resp, nil
		}
		
		// 检查错误是否继续重试
		if !options.OnError(err) {
			return nil, err
		}
		
		lastErr = err
		time.Sleep(options.RetryDelay)
	}
	
	return nil, fmt.Errorf("所有代理重试均失败，最后错误: %v", lastErr)
}

// CreateHTTPClientWithFallback 创建支持代理fallback的HTTP客户端
func CreateHTTPClientWithFallback(originalProxy string, useProxyPool bool) *http.Client {
	// 如果不使用代理池，使用原始逻辑
	if !useProxyPool {
		return provider.NewHTTPClient(originalProxy, 0)
	}
	
	// 如果有原始代理，先尝试原始代理
	if originalProxy != "" {
		client, err := provider.NewHTTPClientWithProxy(originalProxy, 0)
		if err == nil {
			return client
		}
	}
	
	// 使用代理池
	return provider.NewHTTPClientWithPoolProxy(true, 0)
}