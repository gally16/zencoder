package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"zencoder2api/internal/model"
	"zencoder2api/internal/service"
)

type OpenAIHandler struct {
	svc     *service.OpenAIService
	grokSvc *service.GrokService
}

func NewOpenAIHandler() *OpenAIHandler {
	return &OpenAIHandler{
		svc:     service.NewOpenAIService(),
		grokSvc: service.NewGrokService(),
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
	if zenModel.ProviderID == "xai" {
		// Grok 模型使用 xAI 服务
		if err := h.grokSvc.ChatCompletionsProxy(c.Request.Context(), c.Writer, body); err != nil {
			h.handleError(c, err)
		}
		return
	}

	// 其他模型使用 OpenAI 服务
	if err := h.svc.ChatCompletionsProxy(c.Request.Context(), c.Writer, body); err != nil {
		h.handleError(c, err)
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
