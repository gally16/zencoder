package handler

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"zencoder2api/internal/model"
	"zencoder2api/internal/service"
)

type OpenAIHandler struct {
	svc         *service.OpenAIService
	grokSvc     *service.GrokService
	geminiSvc   *service.GeminiService
	anthropicSvc *service.AnthropicService
}

func NewOpenAIHandler() *OpenAIHandler {
	return &OpenAIHandler{
		svc:         service.NewOpenAIService(),
		grokSvc:     service.NewGrokService(),
		geminiSvc:   service.NewGeminiService(),
		anthropicSvc: service.NewAnthropicService(),
	}
}

// generateTraceID 生成一个随机的 trace ID
func generateTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ChatCompletions 处理 POST /v1/chat/completions
func (h *OpenAIHandler) ChatCompletions(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 解析模型名以确定使用哪个服务
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 根据模型的 ProviderID 分流
	zenModel, exists := model.GetZenModel(req.Model)
	if !exists {
		// 模型不存在，返回错误
		h.handleError(c, service.ErrNoAvailableAccount)
		return
	}

	switch zenModel.ProviderID {
	case "xai":
		// Grok 模型使用 xAI 服务
		if err := h.grokSvc.ChatCompletionsProxy(c.Request.Context(), c.Writer, body); err != nil {
			h.handleError(c, err)
		}
	case "gemini":
		// Gemini 模型需要转换格式并路由到 Gemini 服务
		if err := h.handleGeminiChatCompletions(c, req.Model, body); err != nil {
			h.handleError(c, err)
		}
	case "anthropic":
		// Anthropic 模型需要转换格式并路由到 Anthropic 服务
		if err := h.handleAnthropicChatCompletions(c, req.Model, body); err != nil {
			h.handleError(c, err)
		}
	default:
		// OpenAI 模型使用 OpenAI 服务
		if err := h.svc.ChatCompletionsProxy(c.Request.Context(), c.Writer, body); err != nil {
			h.handleError(c, err)
		}
	}
}

// Responses 处理 POST /v1/responses
func (h *OpenAIHandler) Responses(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.ResponsesProxy(c.Request.Context(), c.Writer, body); err != nil {
		h.handleError(c, err)
	}
}

// handleError 统一处理错误，特别是没有可用账号的错误
func (h *OpenAIHandler) handleError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrNoAvailableAccount) || errors.Is(err, service.ErrNoPermission) {
		traceID := generateTraceID()
		errMsg := fmt.Sprintf("没有可用token（traceid: %s）", traceID)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errMsg})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

// handleGeminiChatCompletions 处理通过 /v1/chat/completions 发送的 Gemini 模型请求
func (h *OpenAIHandler) handleGeminiChatCompletions(c *gin.Context, modelName string, body []byte) error {
	// 解析 OpenAI 格式请求
	var req struct {
		Messages []struct {
			Role    string      `json:"role"`
			Content interface{} `json:"content"`
		} `json:"messages"`
		Stream      bool    `json:"stream"`
		MaxTokens   int     `json:"max_tokens"`
		Temperature float64 `json:"temperature"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}

	// 转换为 Gemini 格式
	geminiContents := make([]map[string]interface{}, 0)
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		} else if role == "system" {
			role = "user" // Gemini 不支持 system role，转为 user
		}

		var parts []map[string]interface{}
		switch content := msg.Content.(type) {
		case string:
			parts = []map[string]interface{}{{"text": content}}
		case []interface{}:
			for _, part := range content {
				if partMap, ok := part.(map[string]interface{}); ok {
					if partMap["type"] == "text" {
						parts = append(parts, map[string]interface{}{"text": partMap["text"]})
					}
				}
			}
		}

		geminiContents = append(geminiContents, map[string]interface{}{
			"role":  role,
			"parts": parts,
		})
	}

	geminiBody := map[string]interface{}{
		"contents": geminiContents,
	}

	// 添加生成配置
	if req.MaxTokens > 0 || req.Temperature > 0 {
		genConfig := map[string]interface{}{}
		if req.MaxTokens > 0 {
			genConfig["maxOutputTokens"] = req.MaxTokens
		}
		if req.Temperature > 0 {
			genConfig["temperature"] = req.Temperature
		}
		geminiBody["generationConfig"] = genConfig
	}

	geminiBodyBytes, err := json.Marshal(geminiBody)
	if err != nil {
		return err
	}

	// 调用 Gemini 服务并转换响应
	if req.Stream {
		return h.streamGeminiToOpenAI(c, modelName, geminiBodyBytes)
	}
	return h.nonStreamGeminiToOpenAI(c, modelName, geminiBodyBytes)
}

// nonStreamGeminiToOpenAI 非流式 Gemini 响应转换为 OpenAI 格式
func (h *OpenAIHandler) nonStreamGeminiToOpenAI(c *gin.Context, modelName string, body []byte) error {
	resp, err := h.geminiSvc.GenerateContent(c.Request.Context(), modelName, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// 解析 Gemini 响应
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		// 如果解析失败，返回原始响应
		c.Data(resp.StatusCode, "application/json", respBody)
		return nil
	}

	// 提取文本内容
	var content string
	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		content = geminiResp.Candidates[0].Content.Parts[0].Text
	}

	// 构造 OpenAI 格式响应
	openaiResp := model.ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().Unix()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   modelName,
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

	c.JSON(http.StatusOK, openaiResp)
	return nil
}

// streamGeminiToOpenAI 流式 Gemini 响应转换为 OpenAI 格式
func (h *OpenAIHandler) streamGeminiToOpenAI(c *gin.Context, modelName string, body []byte) error {
	resp, err := h.geminiSvc.StreamGenerateContent(c.Request.Context(), modelName, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 设置 SSE 响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	reader := bufio.NewReader(resp.Body)
	timestamp := time.Now().Unix()
	id := fmt.Sprintf("chatcmpl-%d", timestamp)
	sentFirstChunk := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// 发送结束标记
				if sentFirstChunk {
					finishChunk := model.ChatCompletionChunk{
						ID:      id,
						Object:  "chat.completion.chunk",
						Created: timestamp,
						Model:   modelName,
						Choices: []model.StreamChoice{
							{
								Index:        0,
								Delta:        model.ChatMessage{},
								FinishReason: stringPtr("stop"),
							},
						},
					}
					finishBytes, _ := json.Marshal(finishChunk)
					fmt.Fprintf(c.Writer, "data: %s\n\n", string(finishBytes))
				}
				fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
				flusher.Flush()
				return nil
			}
			return err
		}

		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" || !strings.HasPrefix(trimmedLine, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:"))
		if data == "[DONE]" {
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			flusher.Flush()
			return nil
		}

		// 解析 Gemini SSE 数据
		var geminiChunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(data), &geminiChunk); err != nil {
			continue
		}

		// 提取文本
		var text string
		if len(geminiChunk.Candidates) > 0 && len(geminiChunk.Candidates[0].Content.Parts) > 0 {
			text = geminiChunk.Candidates[0].Content.Parts[0].Text
		}

		if text != "" {
			delta := model.ChatMessage{Content: text}
			if !sentFirstChunk {
				delta.Role = "assistant"
			}

			chunk := model.ChatCompletionChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: timestamp,
				Model:   modelName,
				Choices: []model.StreamChoice{
					{
						Index:        0,
						Delta:        delta,
						FinishReason: nil,
					},
				},
			}

			chunkBytes, _ := json.Marshal(chunk)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(chunkBytes))
			flusher.Flush()
			sentFirstChunk = true
		}
	}
}

// handleAnthropicChatCompletions 处理通过 /v1/chat/completions 发送的 Anthropic 模型请求
func (h *OpenAIHandler) handleAnthropicChatCompletions(c *gin.Context, modelName string, body []byte) error {
	// 解析 OpenAI 格式请求
	var req struct {
		Messages []struct {
			Role    string      `json:"role"`
			Content interface{} `json:"content"`
		} `json:"messages"`
		Stream      bool    `json:"stream"`
		MaxTokens   int     `json:"max_tokens"`
		Temperature float64 `json:"temperature"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}

	// 转换为 Anthropic 格式
	var systemPrompt string
	anthropicMessages := make([]map[string]interface{}, 0)

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			// Anthropic 使用单独的 system 参数
			switch content := msg.Content.(type) {
			case string:
				systemPrompt = content
			}
			continue
		}

		var contentValue interface{}
		switch content := msg.Content.(type) {
		case string:
			contentValue = content
		case []interface{}:
			// 保持数组格式
			contentValue = content
		default:
			contentValue = msg.Content
		}

		anthropicMessages = append(anthropicMessages, map[string]interface{}{
			"role":    msg.Role,
			"content": contentValue,
		})
	}

	anthropicBody := map[string]interface{}{
		"model":    modelName,
		"messages": anthropicMessages,
		"stream":   req.Stream,
	}

	if systemPrompt != "" {
		anthropicBody["system"] = systemPrompt
	}
	if req.MaxTokens > 0 {
		anthropicBody["max_tokens"] = req.MaxTokens
	} else {
		anthropicBody["max_tokens"] = 4096 // Anthropic 要求必须指定
	}
	if req.Temperature > 0 {
		anthropicBody["temperature"] = req.Temperature
	}

	anthropicBodyBytes, err := json.Marshal(anthropicBody)
	if err != nil {
		return err
	}

	// 调用 Anthropic 服务并转换响应
	if req.Stream {
		return h.streamAnthropicToOpenAI(c, modelName, anthropicBodyBytes)
	}
	return h.nonStreamAnthropicToOpenAI(c, modelName, anthropicBodyBytes)
}

// nonStreamAnthropicToOpenAI 非流式 Anthropic 响应转换为 OpenAI 格式
func (h *OpenAIHandler) nonStreamAnthropicToOpenAI(c *gin.Context, modelName string, body []byte) error {
	resp, err := h.anthropicSvc.Messages(c.Request.Context(), body, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// 解析 Anthropic 响应
	var anthropicResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		// 如果解析失败，返回原始响应
		c.Data(resp.StatusCode, "application/json", respBody)
		return nil
	}

	// 提取文本内容
	var content string
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	// 构造 OpenAI 格式响应
	openaiResp := model.ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().Unix()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   modelName,
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

	c.JSON(http.StatusOK, openaiResp)
	return nil
}

// streamAnthropicToOpenAI 流式 Anthropic 响应转换为 OpenAI 格式
func (h *OpenAIHandler) streamAnthropicToOpenAI(c *gin.Context, modelName string, body []byte) error {
	resp, err := h.anthropicSvc.Messages(c.Request.Context(), body, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 设置 SSE 响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	reader := bufio.NewReader(resp.Body)
	timestamp := time.Now().Unix()
	id := fmt.Sprintf("chatcmpl-%d", timestamp)
	sentFirstChunk := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// 发送结束标记
				if sentFirstChunk {
					finishChunk := model.ChatCompletionChunk{
						ID:      id,
						Object:  "chat.completion.chunk",
						Created: timestamp,
						Model:   modelName,
						Choices: []model.StreamChoice{
							{
								Index:        0,
								Delta:        model.ChatMessage{},
								FinishReason: stringPtr("stop"),
							},
						},
					}
					finishBytes, _ := json.Marshal(finishChunk)
					fmt.Fprintf(c.Writer, "data: %s\n\n", string(finishBytes))
				}
				fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
				flusher.Flush()
				return nil
			}
			return err
		}

		trimmedLine := strings.TrimSpace(line)

		// 跳过 event: 行
		if strings.HasPrefix(trimmedLine, "event:") {
			continue
		}

		if trimmedLine == "" || !strings.HasPrefix(trimmedLine, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:"))

		// 解析 Anthropic SSE 数据
		var anthropicEvent struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			ContentBlock struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(data), &anthropicEvent); err != nil {
			continue
		}

		var text string
		if anthropicEvent.Type == "content_block_delta" && anthropicEvent.Delta.Type == "text_delta" {
			text = anthropicEvent.Delta.Text
		}

		if text != "" {
			delta := model.ChatMessage{Content: text}
			if !sentFirstChunk {
				delta.Role = "assistant"
			}

			chunk := model.ChatCompletionChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: timestamp,
				Model:   modelName,
				Choices: []model.StreamChoice{
					{
						Index:        0,
						Delta:        delta,
						FinishReason: nil,
					},
				},
			}

			chunkBytes, _ := json.Marshal(chunk)
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(chunkBytes))
			flusher.Flush()
			sentFirstChunk = true
		}
	}
}

// stringPtr 返回字符串指针
func stringPtr(s string) *string {
	return &s
}
