package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"zencoder2api/internal/model"
	"zencoder2api/internal/service/provider"
)

const OpenAIBaseURL = "https://api.zencoder.ai/openai"

type OpenAIService struct{}

func NewOpenAIService() *OpenAIService {
	return &OpenAIService{}
}

// ChatCompletions 处理/v1/chat/completions请求
func (s *OpenAIService) ChatCompletions(ctx context.Context, body []byte) (*http.Response, error) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}

	// 检查模型是否存在于模型字典中
	_, exists := model.GetZenModel(req.Model)
	if !exists {
		DebugLog(ctx, "[OpenAI] 模型不存在: %s", req.Model)
		return nil, ErrNoAvailableAccount
	}

	DebugLogRequest(ctx, "OpenAI", "/v1/chat/completions", req.Model)

	var lastErr error
	for i := 0; i < MaxRetries; i++ {
		account, err := GetNextAccountForModel(req.Model)
		if err != nil {
			DebugLogRequestEnd(ctx, "OpenAI", false, err)
			return nil, err
		}
		DebugLogAccountSelected(ctx, "OpenAI", account.ID, account.Email)

		// Zencoder API使用/v1/responses端点
		// 需要转换请求体：messages -> input
		convertedBody, err := s.convertChatToResponsesBody(body)
		if err != nil {
			DebugLogRequestEnd(ctx, "OpenAI", false, err)
			return nil, fmt.Errorf("failed to convert request body: %w", err)
		}

		resp, err := s.doRequest(ctx, account, req.Model, "/v1/responses", convertedBody)
		if err != nil {
			MarkAccountError(account)
			lastErr = err
			DebugLogRetry(ctx, "OpenAI", i+1, account.ID, err)
			continue
		}

		DebugLogResponseReceived(ctx, "OpenAI", resp.StatusCode)
		DebugLogResponseHeaders(ctx, "OpenAI", resp.Header)
		
		// 总是输出重要的响应头信息
		if resp.Header.Get("Zen-Pricing-Period-Limit") != "" ||
		   resp.Header.Get("Zen-Pricing-Period-Cost") != "" ||
		   resp.Header.Get("Zen-Request-Cost") != "" {
			log.Printf("[OpenAI] 积分信息 - 周期限额: %s, 周期消耗: %s, 本次消耗: %s",
				resp.Header.Get("Zen-Pricing-Period-Limit"),
				resp.Header.Get("Zen-Pricing-Period-Cost"),
				resp.Header.Get("Zen-Request-Cost"))
		}

		if resp.StatusCode >= 400 {
			// 读取错误响应内容
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			DebugLogErrorResponse(ctx, "OpenAI", resp.StatusCode, string(errBody))

			// 400和500错误直接返回，不进行账号错误计数
			if resp.StatusCode == 400 || resp.StatusCode == 500 {
				DebugLogRequestEnd(ctx, "OpenAI", false, fmt.Errorf("API error: %d", resp.StatusCode))
				return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(errBody))
			}

			// 429 错误特殊处理
			if resp.StatusCode == 429 {
				log.Printf("[OpenAI] 429限流错误，尝试使用代理重试")

				// 尝试使用代理池重试
				proxyResp, proxyErr := s.retryWithProxy(ctx, account, req.Model, "/v1/responses", convertedBody)
				if proxyErr == nil && proxyResp != nil {
					// 代理重试成功
					return proxyResp, nil
				}

				log.Printf("[OpenAI] 代理重试失败: %v", proxyErr)
				MarkAccountRateLimitedWithResponse(account, resp)
			} else {
				MarkAccountError(account)
			}
			
			lastErr = fmt.Errorf("API error: %d", resp.StatusCode)
			DebugLogRetry(ctx, "OpenAI", i+1, account.ID, lastErr)
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
		
		DebugLogRequestEnd(ctx, "OpenAI", true, nil)
		return resp, nil
	}

	DebugLogRequestEnd(ctx, "OpenAI", false, lastErr)
	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

// Responses 处理/v1/responses请求
func (s *OpenAIService) Responses(ctx context.Context, body []byte) (*http.Response, error) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}

	// 检查模型是否存在于模型字典中
	_, exists := model.GetZenModel(req.Model)
	if !exists {
		DebugLog(ctx, "[OpenAI] 模型不存在: %s", req.Model)
		return nil, ErrNoAvailableAccount
	}

	DebugLogRequest(ctx, "OpenAI", "/v1/responses", req.Model)

	var lastErr error
	for i := 0; i < MaxRetries; i++ {
		account, err := GetNextAccountForModel(req.Model)
		if err != nil {
			DebugLogRequestEnd(ctx, "OpenAI", false, err)
			return nil, err
		}
		DebugLogAccountSelected(ctx, "OpenAI", account.ID, account.Email)

		resp, err := s.doRequest(ctx, account, req.Model, "/v1/responses", body)
		if err != nil {
			MarkAccountError(account)
			lastErr = err
			DebugLogRetry(ctx, "OpenAI", i+1, account.ID, err)
			continue
		}

		DebugLogResponseReceived(ctx, "OpenAI", resp.StatusCode)
		DebugLogResponseHeaders(ctx, "OpenAI", resp.Header)
		
		// 总是输出重要的响应头信息
		if resp.Header.Get("Zen-Pricing-Period-Limit") != "" ||
		   resp.Header.Get("Zen-Pricing-Period-Cost") != "" ||
		   resp.Header.Get("Zen-Request-Cost") != "" {
			log.Printf("[OpenAI] 积分信息 - 周期限额: %s, 周期消耗: %s, 本次消耗: %s",
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
				log.Printf("[OpenAI] 429限流错误，尝试使用代理重试")

				// 尝试使用代理池重试
				proxyResp, proxyErr := s.retryWithProxy(ctx, account, req.Model, "/v1/responses", body)
				if proxyErr == nil && proxyResp != nil {
					// 代理重试成功
					return proxyResp, nil
				}

				log.Printf("[OpenAI] 代理重试失败: %v", proxyErr)
				// 将账号放入短期冷却（5秒）
				MarkAccountRateLimitedShort(account)
				// 不输出错误日志，直接返回
				return nil, ErrNoAvailableAccount
			}
			
			DebugLogErrorResponse(ctx, "OpenAI", resp.StatusCode, string(errBody))

			// 400和500错误直接返回，不进行账号错误计数
			if resp.StatusCode == 400 || resp.StatusCode == 500 {
				DebugLogRequestEnd(ctx, "OpenAI", false, fmt.Errorf("API error: %d", resp.StatusCode))
				return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(errBody))
			}

			MarkAccountError(account)
			lastErr = fmt.Errorf("API error: %d", resp.StatusCode)
			DebugLogRetry(ctx, "OpenAI", i+1, account.ID, lastErr)
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
		
		DebugLogRequestEnd(ctx, "OpenAI", true, nil)
		return resp, nil
	}

	DebugLogRequestEnd(ctx, "OpenAI", false, lastErr)
	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

// convertChatToResponsesBody 将 Chat Completion 的请求体转换为 Responses API 的请求体
func (s *OpenAIService) convertChatToResponsesBody(body []byte) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	// 移除 /v1/responses API 不支持的参数
	delete(raw, "stream_options")  // 不支持 stream_options.include_usage 等
	delete(raw, "function_call")   // 旧版函数调用参数
	delete(raw, "functions")       // 旧版函数定义参数
	
	// 转换 token 限制参数
	// max_completion_tokens (新) / max_tokens (旧) -> max_output_tokens (Responses API)
	if val, ok := raw["max_completion_tokens"]; ok {
		raw["max_output_tokens"] = val
		delete(raw, "max_completion_tokens")
	} else if val, ok := raw["max_tokens"]; ok {
		raw["max_output_tokens"] = val
		delete(raw, "max_tokens")
	}

	modelStr, _ := raw["model"].(string)

	// 检查是否有 messages 字段
	if messages, ok := raw["messages"].([]interface{}); ok {
		if modelStr == "gpt-5-nano-2025-08-07" {
			// gpt-5-nano 特殊处理：转换为复杂的 input 结构
			newInput := make([]map[string]interface{}, 0)
			for _, m := range messages {
				if msgMap, ok := m.(map[string]interface{}); ok {
					role, _ := msgMap["role"].(string)
					content := msgMap["content"]

					newItem := map[string]interface{}{
						"type": "message",
						"role": role,
					}

					newContent := make([]map[string]interface{}, 0)
					if contentStr, ok := content.(string); ok {
						newContent = append(newContent, map[string]interface{}{
							"type": "input_text",
							"text": contentStr,
						})
					}
					// 这里的 content 如果是数组，暂时忽略或假设是纯文本场景
					// 如果需要支持多模态，需要进一步解析 content 数组

					newItem["content"] = newContent
					newInput = append(newInput, newItem)
				}
			}
			raw["input"] = newInput
		} else {
			// 标准转换：直接移动到 input
			raw["input"] = messages
		}
		delete(raw, "messages")
	}

	// gpt-5-nano-2025-08-07 特殊处理参数
	if modelStr == "gpt-5-nano-2025-08-07" {
		// 添加该模型所需的特定参数
		raw["prompt_cache_key"] = "generate-name"
		raw["store"] = false
		raw["include"] = []string{"reasoning.encrypted_content"}
		raw["service_tier"] = "auto"
	}

	return json.Marshal(raw)
}

func (s *OpenAIService) doRequest(ctx context.Context, account *model.Account, modelID, path string, body []byte) (*http.Response, error) {
	zenModel, exists := model.GetZenModel(modelID)
	if !exists {
		return nil, ErrNoAvailableAccount
	}
	httpClient := provider.NewHTTPClient(account.Proxy, 0)

	// 将模型参数合并到请求体中
	modifiedBody := body
	if zenModel.Parameters != nil {
		var raw map[string]interface{}
		if json.Unmarshal(modifiedBody, &raw) == nil {
			// 添加 reasoning 配置
			if zenModel.Parameters.Reasoning != nil && raw["reasoning"] == nil {
				reasoningMap := map[string]interface{}{
					"effort": zenModel.Parameters.Reasoning.Effort,
				}
				if zenModel.Parameters.Reasoning.Summary != "" {
					reasoningMap["summary"] = zenModel.Parameters.Reasoning.Summary
				}
				raw["reasoning"] = reasoningMap
			}
			
			// 添加 text 配置
			if zenModel.Parameters.Text != nil && raw["text"] == nil {
				raw["text"] = map[string]interface{}{
					"verbosity": zenModel.Parameters.Text.Verbosity,
				}
			}
			
			// 添加 temperature 配置
			if zenModel.Parameters.Temperature != nil && raw["temperature"] == nil {
				raw["temperature"] = *zenModel.Parameters.Temperature
			}
			
			modifiedBody, _ = json.Marshal(raw)
		}
	}

	// gpt-5-nano-2025-08-07 特殊处理参数
	if modelID == "gpt-5-nano-2025-08-07" {
		var raw map[string]interface{}
		if json.Unmarshal(modifiedBody, &raw) == nil {
			// 添加 text 参数
			if _, ok := raw["text"]; !ok {
				raw["text"] = map[string]string{"verbosity": "medium"}
			}
			// 添加 temperature 参数 (如果缺失)
			if _, ok := raw["temperature"]; !ok {
				raw["temperature"] = 1
			}
			// 强制开启 stream，因为该模型似乎不支持非流式
			raw["stream"] = true

			// 修正 reasoning 参数，添加 summary
			if reasoning, ok := raw["reasoning"].(map[string]interface{}); ok {
				reasoning["summary"] = "auto"
				raw["reasoning"] = reasoning
			} else {
				raw["reasoning"] = map[string]interface{}{
					"effort":  "minimal",
					"summary": "auto",
				}
			}
			modifiedBody, _ = json.Marshal(raw)
		}
	}

	// 注意：已移除模型重定向逻辑，直接使用用户请求的模型名
	DebugLogActualModel(ctx, "OpenAI", modelID, modelID)

	reqURL := OpenAIBaseURL + path
	DebugLogRequestSent(ctx, "OpenAI", reqURL)

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
	DebugLogRequestHeaders(ctx, "OpenAI", httpReq.Header)
	
	// 强制记录请求体用于调试
	log.Printf("[DEBUG] [OpenAI] 请求体:")
	log.Printf("[DEBUG] [OpenAI] %s", string(modifiedBody))

	return httpClient.Do(httpReq)
}

// ChatCompletionsProxy 代理chat completions请求
func (s *OpenAIService) ChatCompletionsProxy(ctx context.Context, w http.ResponseWriter, body []byte) error {
	// 解析 model 和 stream 参数
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	// 忽略错误，因为ChatCompletions会再次解析并处理错误
	_ = json.Unmarshal(body, &req)

	resp, err := s.ChatCompletions(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if req.Stream {
		return s.streamConvertedResponse(w, resp, req.Model)
	}

	return s.handleNonStreamResponse(w, resp, req.Model)
}

func (s *OpenAIService) handleNonStreamResponse(w http.ResponseWriter, resp *http.Response, modelID string) error {
	// 读取全部响应体
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// 复制响应头
	for k, v := range resp.Header {
		// 过滤掉 Content-Length (会重新计算) 和 Content-Encoding (Go会自动解压)
		if k != "Content-Length" && k != "Content-Encoding" {
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}
	}
	w.WriteHeader(resp.StatusCode)

	// 尝试解析响应
	var raw map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		// 如果不是 JSON，检查是否是 SSE 流 (可能是因为我们强制开启了 stream)
		bodyStr := string(bodyBytes)
		trimmedBody := strings.TrimSpace(bodyStr)
		contentType := resp.Header.Get("Content-Type")
		isSSE := strings.Contains(contentType, "text/event-stream") ||
			strings.HasPrefix(trimmedBody, "data:") ||
			strings.HasPrefix(trimmedBody, "event:") ||
			strings.HasPrefix(trimmedBody, ":") ||
			modelID == "gpt-5-nano-2025-08-07" // 强制该模型走 SSE 解析

		if isSSE {
			var fullContent string
			scanner := bufio.NewScanner(bytes.NewReader(bodyBytes))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				data := strings.TrimPrefix(line, "data: ")
				if data == "[DONE]" {
					break
				}
				var chunk map[string]interface{}
				if json.Unmarshal([]byte(data), &chunk) == nil {
					// 尝试提取 content
					if val, ok := chunk["text"].(string); ok {
						fullContent += val
					} else if val, ok := chunk["content"].(string); ok {
						fullContent += val
					} else if val, ok := chunk["response"].(string); ok {
						fullContent += val
					}
					// 标准 chunk
					if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
						if choice, ok := choices[0].(map[string]interface{}); ok {
							if delta, ok := choice["delta"].(map[string]interface{}); ok {
								if content, ok := delta["content"].(string); ok {
									fullContent += content
								}
							}
						}
					}
				}
			}

			// 如果提取到了内容，或者是强制模型（即使没提取到也返回空内容以避免透传错误格式）
			if fullContent != "" || modelID == "gpt-5-nano-2025-08-07" {
				timestamp := time.Now().Unix()
				respObj := model.ChatCompletionResponse{
					ID:      fmt.Sprintf("chatcmpl-%d", timestamp),
					Object:  "chat.completion",
					Created: timestamp,
					Model:   modelID,
					Choices: []model.Choice{
						{
							Index: 0,
							Message: model.ChatMessage{
								Role:    "assistant",
								Content: fullContent,
							},
							FinishReason: "stop",
						},
					},
				}
				return json.NewEncoder(w).Encode(respObj)
			}
		}

		// 既不是 JSON 也不是 SSE，直接透传
		w.Write(bodyBytes)
		return nil
	}

	// 检查是否已经是 OpenAI 格式 (包含 choices)
	if _, ok := raw["choices"]; ok {
		w.Write(bodyBytes)
		return nil
	}

	// 尝试从常见字段提取内容进行转换
	var content string
	if val, ok := raw["text"].(string); ok {
		content = val
	} else if val, ok := raw["content"].(string); ok {
		content = val
	} else if val, ok := raw["response"].(string); ok {
		content = val
	}

	if content != "" {
		timestamp := time.Now().Unix()
		respObj := model.ChatCompletionResponse{
			ID:      fmt.Sprintf("chatcmpl-%d", timestamp),
			Object:  "chat.completion",
			Created: timestamp,
			Model:   modelID,
			Choices: []model.Choice{
				{
					Index: 0,
					Message: model.ChatMessage{
						Role:    "assistant",
						Content: content,
					},
					FinishReason: "stop",
				},
			},
		}
		return json.NewEncoder(w).Encode(respObj)
	}

	// 无法识别格式，直接透传
	w.Write(bodyBytes)
	return nil
}

func (s *OpenAIService) streamConvertedResponse(w http.ResponseWriter, resp *http.Response, modelID string) error {
	// 复制响应头
	for k, v := range resp.Header {
		// 过滤掉 Content-Encoding 和 Content-Length
		if k != "Content-Encoding" && k != "Content-Length" {
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		// 如果不支持Flusher，回退到普通复制
		_, err := io.Copy(w, resp.Body)
		return err
	}

	reader := bufio.NewReader(resp.Body)
	timestamp := time.Now().Unix()
	id := fmt.Sprintf("chatcmpl-%d", timestamp)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		// 处理空行
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			fmt.Fprintf(w, "\n")
			flusher.Flush()
			continue
		}

		// 解析 data: 前缀
		if !strings.HasPrefix(trimmedLine, "data: ") {
			// 尝试解析为 JSON 对象 (处理被强制转为非流式的响应)
			var rawObj map[string]interface{}
			if json.Unmarshal([]byte(trimmedLine), &rawObj) == nil {
				// 尝试从 JSON 中提取内容
				var content string
				if val, ok := rawObj["text"].(string); ok {
					content = val
				} else if val, ok := rawObj["content"].(string); ok {
					content = val
				} else if val, ok := rawObj["response"].(string); ok {
					content = val
				}

				if content != "" {
					// 构造并发送 SSE chunk
					chunk := model.ChatCompletionChunk{
						ID:      id,
						Object:  "chat.completion.chunk",
						Created: timestamp,
						Model:   modelID,
						Choices: []model.StreamChoice{
							{
								Index: 0,
								Delta: model.ChatMessage{
									Content: content,
								},
								FinishReason: nil,
							},
						},
					}
					newBytes, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", string(newBytes))

					// 发送结束标记
					fmt.Fprintf(w, "data: [DONE]\n\n")
					flusher.Flush()
					return nil
				}
			}

			// 非 data 行直接通过
			fmt.Fprint(w, line)
			flusher.Flush()
			continue
		}

		data := strings.TrimPrefix(trimmedLine, "data: ")
		if data == "[DONE]" {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return nil
		}

		// 尝试解析 JSON
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			// 解析失败，直接透传
			fmt.Fprint(w, line)
			flusher.Flush()
			continue
		}

		// 检查是否已经是 OpenAI 格式
		if _, hasChoices := raw["choices"]; hasChoices {
			fmt.Fprint(w, line)
			flusher.Flush()
			continue
		}

		// 尝试转换非标准格式
		// 假设可能有 text, content, response 等字段
		var content string
		if val, ok := raw["text"].(string); ok {
			content = val
		} else if val, ok := raw["content"].(string); ok {
			content = val
		} else if val, ok := raw["response"].(string); ok {
			content = val
		}

		if content != "" {
			// 构造标准 OpenAI Chunk
			chunk := model.ChatCompletionChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: timestamp,
				Model:   modelID,
				Choices: []model.StreamChoice{
					{
						Index: 0,
						Delta: model.ChatMessage{
							Content: content,
						},
						FinishReason: nil,
					},
				},
			}

			newBytes, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(newBytes))
			flusher.Flush()
		} else {
			// 无法识别内容，直接透传
			fmt.Fprint(w, line)
			flusher.Flush()
		}
	}
}

// ResponsesProxy 代理responses请求
func (s *OpenAIService) ResponsesProxy(ctx context.Context, w http.ResponseWriter, body []byte) error {
	resp, err := s.Responses(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return StreamResponse(w, resp)
}

// retryWithProxy 使用代理池重试OpenAI请求
func (s *OpenAIService) retryWithProxy(ctx context.Context, account *model.Account, modelID, path string, body []byte) (*http.Response, error) {
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

		log.Printf("[OpenAI] 尝试代理 %s (重试 %d/%d)", proxyURL, i+1, maxRetries)

		// 创建使用代理的HTTP客户端
		proxyClient, err := provider.NewHTTPClientWithProxy(proxyURL, 0)
		if err != nil {
			log.Printf("[OpenAI] 创建代理客户端失败: %v", err)
			continue
		}

		// 将模型参数合并到请求体中
		modifiedBody := body
		if zenModel.Parameters != nil {
			var raw map[string]interface{}
			if json.Unmarshal(modifiedBody, &raw) == nil {
				// 添加 reasoning 配置
				if zenModel.Parameters.Reasoning != nil && raw["reasoning"] == nil {
					reasoningMap := map[string]interface{}{
						"effort": zenModel.Parameters.Reasoning.Effort,
					}
					if zenModel.Parameters.Reasoning.Summary != "" {
						reasoningMap["summary"] = zenModel.Parameters.Reasoning.Summary
					}
					raw["reasoning"] = reasoningMap
				}
				
				// 添加 text 配置
				if zenModel.Parameters.Text != nil && raw["text"] == nil {
					raw["text"] = map[string]interface{}{
						"verbosity": zenModel.Parameters.Text.Verbosity,
					}
				}
				
				// 添加 temperature 配置
				if zenModel.Parameters.Temperature != nil && raw["temperature"] == nil {
					raw["temperature"] = *zenModel.Parameters.Temperature
				}
				
				modifiedBody, _ = json.Marshal(raw)
			}
		}

		// 特殊模型的额外处理
		if modelID == "gpt-5-nano-2025-08-07" {
			var raw map[string]interface{}
			if json.Unmarshal(modifiedBody, &raw) == nil {
				if _, ok := raw["text"]; !ok {
					raw["text"] = map[string]string{"verbosity": "medium"}
				}
				if _, ok := raw["temperature"]; !ok {
					raw["temperature"] = 1
				}
				raw["stream"] = true
				if reasoning, ok := raw["reasoning"].(map[string]interface{}); ok {
					reasoning["summary"] = "auto"
					raw["reasoning"] = reasoning
				} else {
					raw["reasoning"] = map[string]interface{}{
						"effort":  "minimal",
						"summary": "auto",
					}
				}
				modifiedBody, _ = json.Marshal(raw)
			}
		}

		// 创建新请求
		reqURL := OpenAIBaseURL + path
		httpReq, err := http.NewRequest("POST", reqURL, bytes.NewReader(modifiedBody))
		if err != nil {
			log.Printf("[OpenAI] 创建请求失败: %v", err)
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

		// 强制记录代理请求体用于调试
		log.Printf("[DEBUG] [OpenAI] 代理请求体:")
		log.Printf("[DEBUG] [OpenAI] %s", string(modifiedBody))

		// 执行请求
		resp, err := proxyClient.Do(httpReq)
		if err != nil {
			log.Printf("[OpenAI] 代理请求失败: %v", err)
			continue
		}

		// 检查响应状态
		if resp.StatusCode == 429 {
			// 仍然是429，尝试下一个代理
			resp.Body.Close()
			log.Printf("[OpenAI] 代理 %s 仍返回429，尝试下一个", proxyURL)
			continue
		}

		if resp.StatusCode >= 400 {
			// 其他错误，记录并尝试下一个代理
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Printf("[OpenAI] 代理 %s 返回错误 %d: %s", proxyURL, resp.StatusCode, string(errBody))
			continue
		}

		// 成功
		log.Printf("[OpenAI] 代理 %s 请求成功", proxyURL)
		return resp, nil
	}

	return nil, fmt.Errorf("所有代理重试均失败")
}
