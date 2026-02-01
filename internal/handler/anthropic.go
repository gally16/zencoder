package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"

	"zencoder2api/internal/service"

	"github.com/gin-gonic/gin"
)

type AnthropicHandler struct {
	svc *service.AnthropicService
}

func NewAnthropicHandler() *AnthropicHandler {
	return &AnthropicHandler{svc: service.NewAnthropicService()}
}

// generateTraceID 生成一个随机的 trace ID
func generateAnthropicTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Messages 处理 POST /v1/messages
func (h *AnthropicHandler) Messages(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 传递原始请求头给service层，用于错误日志记录
	ctx := context.WithValue(c.Request.Context(), "originalHeaders", c.Request.Header)
	
	if err := h.svc.MessagesProxy(ctx, c.Writer, body); err != nil {
		h.handleError(c, err)
	}
}

// handleError 统一处理错误，特别是没有可用账号的错误
func (h *AnthropicHandler) handleError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrNoAvailableAccount) || errors.Is(err, service.ErrNoPermission) {
		traceID := generateAnthropicTraceID()
		errMsg := fmt.Sprintf("没有可用token（traceid: %s）", traceID)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errMsg})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
