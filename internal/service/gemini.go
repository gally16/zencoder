package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"

	"zencoder2api/internal/model"
	"zencoder2api/internal/service/provider"
)

const GeminiBaseURL = "https://api.zencoder.ai/gemini"

type GeminiService struct{}

func NewGeminiService() *GeminiService {
	return &GeminiService{}
}

// GenerateContent 处理generateContent请求
func (s *GeminiService) GenerateContent(ctx context.Context, modelName string, body []byte) (*http.Response, error) {
	// 检查模型是否存在于模型字典中
	_, exists := model.GetZenModel(modelName)
	if !exists {
		DebugLog(ctx, "[Gemini] 模型不存在: %s", modelName)
		return nil, ErrNoAvailableAccount
	}

	DebugLogRequest(ctx, "Gemini", "generateContent", modelName)

	var lastErr error
	for i := 0; i < MaxRetries; i++ {
		account, err := GetNextAccountForModel(modelName)
		if err != nil {
			DebugLogRequestEnd(ctx, "Gemini", false, err)
			return nil, err
		}
		DebugLogAccountSelected(ctx, "Gemini", account.ID, account.Email)

		resp, err := s.doRequest(ctx, account, modelName, body, false)
		if err != nil {
			MarkAccountError(account)
			lastErr = err
			DebugLogRetry(ctx, "Gemini", i+1, account.ID, err)
			continue
		}

		DebugLogResponseReceived(ctx, "Gemini", resp.StatusCode)
		DebugLogResponseHeaders(ctx, "Gemini", resp.Header)
		
		// 总是输出重要的响应头信息
		if resp.Header.Get("Zen-Pricing-Period-Limit") != "" ||
		   resp.Header.Get("Zen-Pricing-Period-Cost") != "" ||
		   resp.Header.Get("Zen-Request-Cost") != "" {
			log.Printf("[Gemini] 积分信息 - 周期限额: %s, 周期消耗: %s, 本次消耗: %s",
				resp.Header.Get("Zen-Pricing-Period-Limit"),
				resp.Header.Get("Zen-Pricing-Period-Cost"),
				resp.Header.Get("Zen-Request-Cost"))
		}

		if resp.StatusCode >= 400 {
			// 读取错误响应内容用于日志
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			DebugLogErrorResponse(ctx, "Gemini", resp.StatusCode, string(errBody))

			// 400和500错误直接返回，不进行账号错误计数
			if resp.StatusCode == 400 || resp.StatusCode == 500 {
				DebugLogRequestEnd(ctx, "Gemini", false, fmt.Errorf("API error: %d", resp.StatusCode))
				return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(errBody))
			}

			// 429 错误特殊处理
			if resp.StatusCode == 429 {
				log.Printf("[Gemini] 429限流错误，尝试使用代理重试")

				// 尝试使用代理池重试
				proxyResp, proxyErr := s.retryWithProxy(ctx, account, modelName, body, false)
				if proxyErr == nil && proxyResp != nil {
					// 代理重试成功
					return proxyResp, nil
				}

				log.Printf("[Gemini] 代理重试失败: %v", proxyErr)
				MarkAccountRateLimitedWithResponse(account, resp)
			} else {
				MarkAccountError(account)
			}
			
			lastErr = fmt.Errorf("API error: %d", resp.StatusCode)
			DebugLogRetry(ctx, "Gemini", i+1, account.ID, lastErr)
			continue
		}

		ResetAccountError(account)
		zenModel, exists := model.GetZenModel(modelName)
		if !exists {
			// 模型不存在，使用默认倍率
			UpdateAccountCreditsFromResponse(account, resp, 1.0)
		} else {
			// 使用统一的积分更新函数，自动处理响应头中的积分信息
			UpdateAccountCreditsFromResponse(account, resp, zenModel.Multiplier)
		}
		
		DebugLogRequestEnd(ctx, "Gemini", true, nil)
		return resp, nil
	}

	DebugLogRequestEnd(ctx, "Gemini", false, lastErr)
	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

// StreamGenerateContent 处理streamGenerateContent请求
func (s *GeminiService) StreamGenerateContent(ctx context.Context, modelName string, body []byte) (*http.Response, error) {
	// 检查模型是否存在于模型字典中
	_, exists := model.GetZenModel(modelName)
	if !exists {
		DebugLog(ctx, "[Gemini] 模型不存在: %s", modelName)
		return nil, ErrNoAvailableAccount
	}

	DebugLogRequest(ctx, "Gemini", "streamGenerateContent", modelName)

	var lastErr error
	for i := 0; i < MaxRetries; i++ {
		account, err := GetNextAccountForModel(modelName)
		if err != nil {
			DebugLogRequestEnd(ctx, "Gemini", false, err)
			return nil, err
		}
		DebugLogAccountSelected(ctx, "Gemini", account.ID, account.Email)

		resp, err := s.doRequest(ctx, account, modelName, body, true)
		if err != nil {
			MarkAccountError(account)
			lastErr = err
			DebugLogRetry(ctx, "Gemini", i+1, account.ID, err)
			continue
		}

		DebugLogResponseReceived(ctx, "Gemini", resp.StatusCode)
		DebugLogResponseHeaders(ctx, "Gemini", resp.Header)
		
		// 总是输出重要的响应头信息
		if resp.Header.Get("Zen-Pricing-Period-Limit") != "" ||
		   resp.Header.Get("Zen-Pricing-Period-Cost") != "" ||
		   resp.Header.Get("Zen-Request-Cost") != "" {
			log.Printf("[Gemini] 积分信息 - 周期限额: %s, 周期消耗: %s, 本次消耗: %s",
				resp.Header.Get("Zen-Pricing-Period-Limit"),
				resp.Header.Get("Zen-Pricing-Period-Cost"),
				resp.Header.Get("Zen-Request-Cost"))
		}

		if resp.StatusCode >= 400 {
			// 读取错误响应内容用于日志
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			DebugLogErrorResponse(ctx, "Gemini", resp.StatusCode, string(errBody))

			// 400和500错误直接返回，不进行账号错误计数
			if resp.StatusCode == 400 || resp.StatusCode == 500 {
				DebugLogRequestEnd(ctx, "Gemini", false, fmt.Errorf("API error: %d", resp.StatusCode))
				return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(errBody))
			}

			// 429 错误特殊处理
			if resp.StatusCode == 429 {
				log.Printf("[Gemini] 429限流错误，尝试使用代理重试")

				// 尝试使用代理池重试
				proxyResp, proxyErr := s.retryWithProxy(ctx, account, modelName, body, true)
				if proxyErr == nil && proxyResp != nil {
					// 代理重试成功
					return proxyResp, nil
				}

				log.Printf("[Gemini] 代理重试失败: %v", proxyErr)
				MarkAccountRateLimitedWithResponse(account, resp)
			} else {
				MarkAccountError(account)
			}
			
			lastErr = fmt.Errorf("API error: %d", resp.StatusCode)
			DebugLogRetry(ctx, "Gemini", i+1, account.ID, lastErr)
			continue
		}

		ResetAccountError(account)
		zenModel, exists := model.GetZenModel(modelName)
		if !exists {
			// 模型不存在，使用默认倍率
			UseCredit(account, 1.0)
		} else {
			// 流式响应，暂时使用模型倍率（因为没有完整响应头）
			UseCredit(account, zenModel.Multiplier)
		}
		
		DebugLogRequestEnd(ctx, "Gemini", true, nil)
		return resp, nil
	}

	DebugLogRequestEnd(ctx, "Gemini", false, lastErr)
	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

func (s *GeminiService) doRequest(ctx context.Context, account *model.Account, modelName string, body []byte, stream bool) (*http.Response, error) {
	zenModel, exists := model.GetZenModel(modelName)
	if !exists {
		return nil, ErrNoAvailableAccount
	}
	httpClient := provider.NewHTTPClient(account.Proxy, 0)

	action := "generateContent"
	queryParam := ""
	if stream {
		action = "streamGenerateContent"
		queryParam = "?alt=sse"
	}
	reqURL := fmt.Sprintf("%s/v1beta/models/%s:%s%s", GeminiBaseURL, modelName, action, queryParam)
	DebugLogRequestSent(ctx, "Gemini", reqURL)
	httpReq, err := http.NewRequest("POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// 设置Zencoder自定义请求头
	SetZencoderHeaders(httpReq, account, zenModel)

	// 流式请求禁用压缩，确保可以逐行读取
	if stream {
		httpReq.Header.Set("Accept-Encoding", "identity")
	}

	// 添加模型配置的额外请求头
	if zenModel.Parameters != nil && zenModel.Parameters.ExtraHeaders != nil {
		for k, v := range zenModel.Parameters.ExtraHeaders {
			httpReq.Header.Set(k, v)
		}
	}

	// 记录请求头用于调试
	DebugLogRequestHeaders(ctx, "Gemini", httpReq.Header)

	return httpClient.Do(httpReq)
}

// GenerateContentProxy 代理generateContent请求
func (s *GeminiService) GenerateContentProxy(ctx context.Context, w http.ResponseWriter, modelName string, body []byte) error {
	resp, err := s.GenerateContent(ctx, modelName, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return StreamResponse(w, resp)
}

// retryWithProxy 使用代理池重试Gemini请求
func (s *GeminiService) retryWithProxy(ctx context.Context, account *model.Account, modelName string, body []byte, stream bool) (*http.Response, error) {
	// 获取模型配置
	zenModel, exists := model.GetZenModel(modelName)
	if !exists {
		return nil, fmt.Errorf("模型配置不存在: %s", modelName)
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

		log.Printf("[Gemini] 尝试代理 %s (重试 %d/%d)", proxyURL, i+1, maxRetries)

		// 创建使用代理的HTTP客户端
		proxyClient, err := provider.NewHTTPClientWithProxy(proxyURL, 0)
		if err != nil {
			log.Printf("[Gemini] 创建代理客户端失败: %v", err)
			continue
		}

		// 创建新请求
		action := "generateContent"
		queryParam := ""
		if stream {
			action = "streamGenerateContent"
			queryParam = "?alt=sse"
		}
		reqURL := fmt.Sprintf("%s/v1beta/models/%s:%s%s", GeminiBaseURL, modelName, action, queryParam)
		httpReq, err := http.NewRequest("POST", reqURL, bytes.NewReader(body))
		if err != nil {
			log.Printf("[Gemini] 创建请求失败: %v", err)
			continue
		}

		// 设置请求头
		SetZencoderHeaders(httpReq, account, zenModel)

		// 流式请求禁用压缩，确保可以逐行读取
		if stream {
			httpReq.Header.Set("Accept-Encoding", "identity")
		}

		// 添加模型配置的额外请求头
		if zenModel.Parameters != nil && zenModel.Parameters.ExtraHeaders != nil {
			for k, v := range zenModel.Parameters.ExtraHeaders {
				httpReq.Header.Set(k, v)
			}
		}

		// 执行请求
		resp, err := proxyClient.Do(httpReq)
		if err != nil {
			log.Printf("[Gemini] 代理请求失败: %v", err)
			continue
		}

		// 检查响应状态
		if resp.StatusCode == 429 {
			// 仍然是429，尝试下一个代理
			resp.Body.Close()
			log.Printf("[Gemini] 代理 %s 仍返回429，尝试下一个", proxyURL)
			continue
		}

		if resp.StatusCode >= 400 {
			// 其他错误，记录并尝试下一个代理
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Printf("[Gemini] 代理 %s 返回错误 %d: %s", proxyURL, resp.StatusCode, string(errBody))
			continue
		}

		// 成功
		log.Printf("[Gemini] 代理 %s 请求成功", proxyURL)
		return resp, nil
	}

	return nil, fmt.Errorf("所有代理重试均失败")
}

// StreamGenerateContentProxy 代理streamGenerateContent请求
func (s *GeminiService) StreamGenerateContentProxy(ctx context.Context, w http.ResponseWriter, modelName string, body []byte) error {
	resp, err := s.StreamGenerateContent(ctx, modelName, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return StreamResponse(w, resp)
}
