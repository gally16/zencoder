package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"

	"zencoder2api/internal/service"

	"github.com/gin-gonic/gin"
)

type GrokHandler struct {
	svc *service.GrokService
}

func NewGrokHandler() *GrokHandler {
	return &GrokHandler{svc: service.NewGrokService()}
}

// generateTraceID 生成一个随机的 trace ID
func generateGrokTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ChatCompletions 处理 POST /v1/chat/completions (xAI)
func (h *GrokHandler) ChatCompletions(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.ChatCompletionsProxy(c.Request.Context(), c.Writer, body); err != nil {
		h.handleError(c, err)
	}
}

// handleError 统一处理错误，特别是没有可用账号的错误
func (h *GrokHandler) handleError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrNoAvailableAccount) || errors.Is(err, service.ErrNoPermission) {
		traceID := generateGrokTraceID()
		errMsg := fmt.Sprintf("没有可用token（traceid: %s）", traceID)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": errMsg})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
