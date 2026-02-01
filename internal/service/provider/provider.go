package provider

import (
	"io"
	"net/http"

	"zencoder2api/internal/model"
)

// Config Provider配置
type Config struct {
	BaseURL      string            // 自定义请求地址
	APIKey       string            // API密钥
	ExtraHeaders map[string]string // 额外请求头
	Proxy        string            // 代理地址
}

// Provider AI平台提供者接口
type Provider interface {
	// Name 返回提供者名称
	Name() string

	// Chat 非流式聊天
	Chat(req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error)

	// ChatStream 流式聊天
	ChatStream(req *model.ChatCompletionRequest, writer http.ResponseWriter) error

	// ValidateToken 验证token是否有效
	ValidateToken() error
}

// BaseProvider 基础Provider实现
type BaseProvider struct {
	Config Config
	Client *http.Client
}

// SetHeaders 设置通用请求头
func (b *BaseProvider) SetHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 设置额外请求头
	for k, v := range b.Config.ExtraHeaders {
		req.Header.Set(k, v)
	}
}

// StreamResponse 通用流式响应处理
func (b *BaseProvider) StreamResponse(body io.Reader, writer http.ResponseWriter) error {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return ErrStreamNotSupported
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	buf := make([]byte, 4096)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			writer.Write(buf[:n])
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return nil
}
