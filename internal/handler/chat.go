package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"zencoder2api/internal/model"
	"zencoder2api/internal/service"
)

type ChatHandler struct {
	svc *service.APIService
}

func NewChatHandler() *ChatHandler {
	return &ChatHandler{svc: service.NewAPIService()}
}

func (h *ChatHandler) ChatCompletions(c *gin.Context) {
	var req model.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Stream {
		h.handleStream(c, &req)
		return
	}

	resp, err := h.svc.Chat(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *ChatHandler) handleStream(c *gin.Context, req *model.ChatCompletionRequest) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	if err := h.svc.ChatStream(req, c.Writer); err != nil {
		c.SSEvent("error", err.Error())
	}
}
