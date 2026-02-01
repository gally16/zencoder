package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"zencoder2api/internal/model"
	"zencoder2api/internal/service/provider"
)

const GrokBaseURL = "https://api.zencoder.ai/xai"

type GrokService struct{}

func NewGrokService() *GrokService {
	return &GrokService{}
}

// ChatCompletions 处理/v1/chat/completions请求
func (s *GrokService) ChatCompletions(ctx context.Context, body []byte) (*http.Response, error) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}

	// 检查模型是否存在于模型字典中
	_, exists := model.GetZenModel(req.Model)
	if !exists {
		DebugLog(ctx, "[Grok] 模型不存在: %s", req.Model)
		return nil, ErrNoAvailableAccount
	}

	DebugLogRequest(ctx, "Grok", "/v1/chat/completions", req.Model)

	var lastErr error
	for i := 0; i < MaxRetries; i++ {
		account, err := GetNextAccountForModel(req.Model)
		if err != nil {
			DebugLogRequestEnd(ctx, "Grok", false, err)
			return nil, err
		}
		DebugLogAccountSelected(ctx, "Grok", account.ID, account.Email)

		resp, err := s.doRequest(ctx, account, req.Model, body)
		if err != nil {
			MarkAccountError(account)
			lastErr = err
			DebugLogRetry(ctx, "Grok", i+1, account.ID, err)
			continue
		}

		DebugLogResponseReceived(ctx, "Grok", resp.StatusCode)
		DebugLogResponseHeaders(ctx, "Grok", resp.Header)
		
		// 总是输出重要的响应头信息
		if resp.Header.Get("Zen-Pricing-Period-Limit") != "" ||
		   resp.Header.Get("Zen-Pricing-Period-Cost") != "" ||
		   resp.Header.Get("Zen-Request-Cost") != "" {
			log.Printf("[Grok] 积分信息 - 周期限额: %s, 周期消耗: %s, 本次消耗: %s",
				resp.Header.Get("Zen-Pricing-Period-Limit"),
				resp.Header.Get("Zen-Pricing-Period-Cost"),
				resp.Header.Get("Zen-Request-Cost"))
		}

		if resp.StatusCode >= 400 {
			// 读取错误响应内容
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			
			// 429 错误特殊处理 - 直接返回，不重试
			if resp.StatusCode == 429 {
				log.Printf("[Grok] 429限流错误，尝试使用代理重试")

				// 尝试使用代理池重试
				proxyResp, proxyErr := s.retryWithProxy(ctx, account, req.Model, body)
				if proxyErr == nil && proxyResp != nil {
					// 代理重试成功
					return proxyResp, nil
				}

				log.Printf("[Grok] 代理重试失败: %v", proxyErr)
				// 在DEBUG模式下记录详细信息
				DebugLogErrorResponse(ctx, "Grok", resp.StatusCode, string(errBody))
				// 将账号放入短期冷却（5秒）
				MarkAccountRateLimitedShort(account)
				// 标记错误并结束请求
				DebugLogRequestEnd(ctx, "Grok", false, ErrNoAvailableAccount)
				// 返回通用错误
				return nil, ErrNoAvailableAccount
			}
			
			DebugLogErrorResponse(ctx, "Grok", resp.StatusCode, string(errBody))

			// 400和500错误直接返回，不进行账号错误计数
			if resp.StatusCode == 400 || resp.StatusCode == 500 {
				DebugLogRequestEnd(ctx, "Grok", false, fmt.Errorf("API error: %d", resp.StatusCode))
				return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(errBody))
			}

			MarkAccountError(account)
			lastErr = fmt.Errorf("API error: %d", resp.StatusCode)
			DebugLogRetry(ctx, "Grok", i+1, account.ID, lastErr)
			continue
		}

		ResetAccountError(account)
		zenModel, exists := model.GetZenModel(req.Model)
		if !exists {
			// 模型不存在，使用默认倍率
			UpdateAccountCreditsFromResponse(account, resp, 1.0)
		} else {
			// 使用统一的积分更新函数，自动处理响应头中的积分信息
			UpdateAccountCreditsFromResponse(account, resp, zenModel.Multiplier)
		}
		
		DebugLogRequestEnd(ctx, "Grok", true, nil)
		return resp, nil
	}

	DebugLogRequestEnd(ctx, "Grok", false, lastErr)
	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

func (s *GrokService) doRequest(ctx context.Context, account *model.Account, modelID string, body []byte) (*http.Response, error) {
	zenModel, exists := model.GetZenModel(modelID)
	if !exists {
		return nil, ErrNoAvailableAccount
	}
	httpClient := provider.NewHTTPClient(account.Proxy, 0)

	// 处理请求体，Grok Code 模型要求 temperature=0
	modifiedBody := body
	if strings.Contains(modelID, "grok-code") {
		modifiedBody, _ = s.setTemperatureZero(body)
	}

	reqURL := GrokBaseURL + "/v1/chat/completions"
	DebugLogRequestSent(ctx, "Grok", reqURL)

	httpReq, err := http.NewRequest("POST", reqURL, bytes.NewReader(modifiedBody))
	if err != nil {
		return nil, err
	}

	// 设置Zencoder自定义请求头
	SetZencoderHeaders(httpReq, account, zenModel)

	// 添加模型配置的额外请求头
	if zenModel.Parameters != nil && zenModel.Parameters.ExtraHeaders != nil {
		for k, v := range zenModel.Parameters.ExtraHeaders {
			httpReq.Header.Set(k, v)
		}
	}

	// 记录请求头用于调试
	DebugLogRequestHeaders(ctx, "Grok", httpReq.Header)

	return httpClient.Do(httpReq)
}

// setTemperatureZero 设置 temperature=0
func (s *GrokService) setTemperatureZero(body []byte) ([]byte, error) {
	var reqMap map[string]interface{}
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return body, err
	}
	reqMap["temperature"] = 0
	return json.Marshal(reqMap)
}

// ChatCompletionsProxy 代理chat completions请求
func (s *GrokService) ChatCompletionsProxy(ctx context.Context, w http.ResponseWriter, body []byte) error {
	resp, err := s.ChatCompletions(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return StreamResponse(w, resp)
}

// retryWithProxy 使用代理池重试Grok请求
func (s *GrokService) retryWithProxy(ctx context.Context, account *model.Account, modelID string, body []byte) (*http.Response, error) {
	// 获取模型配置
	zenModel, exists := model.GetZenModel(modelID)
	if !exists {
		return nil, fmt.Errorf("模型配置不存在: %s", modelID)
	}

	proxyPool := provider.GetProxyPool()
	if !proxyPool.HasProxies() {
		return nil, fmt.Errorf("没有可用的代理")
	}

	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		// 获取随机代理
		proxyURL := proxyPool.GetRandomProxy()
		if proxyURL == "" {
			continue
		}

		log.Printf("[Grok] 尝试代理 %s (重试 %d/%d)", proxyURL, i+1, maxRetries)

		// 创建使用代理的HTTP客户端
		proxyClient, err := provider.NewHTTPClientWithProxy(proxyURL, 0)
		if err != nil {
			log.Printf("[Grok] 创建代理客户端失败: %v", err)
			continue
		}

		// 处理请求体，Grok Code 模型要求 temperature=0
		modifiedBody := body
		if strings.Contains(modelID, "grok-code") {
			modifiedBody, _ = s.setTemperatureZero(body)
		}

		// 创建新请求
		reqURL := GrokBaseURL + "/v1/chat/completions"
		httpReq, err := http.NewRequest("POST", reqURL, bytes.NewReader(modifiedBody))
		if err != nil {
			log.Printf("[Grok] 创建请求失败: %v", err)
			continue
		}

		// 设置请求头
		SetZencoderHeaders(httpReq, account, zenModel)

		// 添加模型配置的额外请求头
		if zenModel.Parameters != nil && zenModel.Parameters.ExtraHeaders != nil {
			for k, v := range zenModel.Parameters.ExtraHeaders {
				httpReq.Header.Set(k, v)
			}
		}

		// 执行请求
		resp, err := proxyClient.Do(httpReq)
		if err != nil {
			log.Printf("[Grok] 代理请求失败: %v", err)
			continue
		}

		// 检查响应状态
		if resp.StatusCode == 429 {
			// 仍然是429，尝试下一个代理
			resp.Body.Close()
			log.Printf("[Grok] 代理 %s 仍返回429，尝试下一个", proxyURL)
			continue
		}

		if resp.StatusCode >= 400 {
			// 其他错误，记录并尝试下一个代理
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Printf("[Grok] 代理 %s 返回错误 %d: %s", proxyURL, resp.StatusCode, string(errBody))
			continue
		}

		// 成功
		log.Printf("[Grok] 代理 %s 请求成功", proxyURL)
		return resp, nil
	}

	return nil, fmt.Errorf("所有代理重试均失败")
}
