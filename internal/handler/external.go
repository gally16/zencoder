package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"zencoder2api/internal/database"
	"zencoder2api/internal/model"
	"zencoder2api/internal/service"
)

type ExternalHandler struct{}

func NewExternalHandler() *ExternalHandler {
	return &ExternalHandler{}
}

// ExternalTokenRequest 外部API提交token请求结构
type ExternalTokenRequest struct {
	AccessToken  string `json:"access_token"`  // OAuth获取的access_token
	RefreshToken string `json:"refresh_token"` // OAuth获取的refresh_token
	Proxy        string `json:"proxy"`         // 可选的代理设置
}

// ExternalTokenResponse 外部API响应结构
type ExternalTokenResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Account *model.Account `json:"account,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// SubmitTokens 外部API接口：接收OAuth token信息并生成账号记录
func (h *ExternalHandler) SubmitTokens(c *gin.Context) {
	var req ExternalTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ExternalTokenResponse{
			Success: false,
			Error:   "请求格式错误: " + err.Error(),
		})
		return
	}

	// 验证必要字段
	if req.AccessToken == "" && req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, ExternalTokenResponse{
			Success: false,
			Error:   "必须提供 access_token 或 refresh_token",
		})
		return
	}

	log.Printf("[外部API] 收到token提交请求，access_token长度: %d, refresh_token长度: %d", 
		len(req.AccessToken), len(req.RefreshToken))

	// 优先使用 access_token，如果同时提供了两个字段
	var masterToken string
	
	if req.AccessToken != "" {
		// 直接使用提供的 access_token
		masterToken = req.AccessToken
		log.Printf("[外部API] 使用提供的 access_token")
	} else {
		// 使用 refresh_token 获取 access_token
		tokenResp, err := service.RefreshAccessToken(req.RefreshToken, req.Proxy)
		if err != nil {
			c.JSON(http.StatusBadRequest, ExternalTokenResponse{
				Success: false,
				Error:   "RefreshToken 无效: " + err.Error(),
			})
			return
		}
		masterToken = tokenResp.AccessToken
		log.Printf("[外部API] 通过 RefreshToken 获取了 access_token")
	}

	log.Printf("[外部API] 开始生成账号凭证")
	
	// 生成凭证
	cred, err := service.GenerateCredential(masterToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ExternalTokenResponse{
			Success: false,
			Error:   fmt.Sprintf("生成失败: %v", err),
		})
		return
	}

	log.Printf("[外部API] 凭证生成成功: ClientID=%s", cred.ClientID)

	// 创建账号
	account := model.Account{
		ClientID:     cred.ClientID,
		ClientSecret: cred.Secret,
		Proxy:        req.Proxy,
		IsActive:     true,
		Status:       "normal",
	}

	// 使用生成的client_id和client_secret获取token，带重试机制
	// 使用OAuth client credentials方式刷新token，使用 https://fe.zencoder.ai/oauth/token
	maxRetries := 3
	retryDelay := 2 * time.Second
	var lastErr error
	
	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("[外部API] 尝试获取token，第 %d/%d 次", attempt, maxRetries)
		
		if _, err := service.RefreshToken(&account); err != nil {
			lastErr = err
			log.Printf("[外部API] 第 %d 次获取token失败: %v", attempt, err)
			
			if attempt < maxRetries {
				log.Printf("[外部API] 等待 %v 后重试", retryDelay)
				time.Sleep(retryDelay)
				continue
			}
		} else {
			log.Printf("[外部API] 第 %d 次获取token成功", attempt)
			lastErr = nil
			break
		}
	}
	
	if lastErr != nil {
		c.JSON(http.StatusBadRequest, ExternalTokenResponse{
			Success: false,
			Error:   fmt.Sprintf("认证失败（重试 %d 次后）: %v", maxRetries, lastErr),
		})
		return
	}

	// 解析 Token 获取详细信息
	if payload, err := service.ParseJWT(account.AccessToken); err == nil {
		account.Email = payload.Email
		account.SubscriptionStartDate = service.GetSubscriptionDate(payload)
		
		if payload.Expiration > 0 {
			account.TokenExpiry = time.Unix(payload.Expiration, 0)
		}

		plan := payload.CustomClaims.Plan
		if plan != "" {
			plan = strings.ToUpper(plan[:1]) + plan[1:]
		}
		if plan != "" {
			account.PlanType = model.PlanType(plan)
		}
	}
	if account.PlanType == "" {
		account.PlanType = model.PlanFree
	}

	// 检查是否已存在
	var existing model.Account
	var count int64
	database.GetDB().Model(&model.Account{}).Where("client_id = ?", account.ClientID).Count(&count)
	if count > 0 {
		// 获取现有账号
		database.GetDB().Where("client_id = ?", account.ClientID).First(&existing)
		// 更新现有账号
		existing.AccessToken = account.AccessToken
		existing.TokenExpiry = account.TokenExpiry
		existing.PlanType = account.PlanType
		existing.Email = account.Email
		existing.SubscriptionStartDate = account.SubscriptionStartDate
		existing.IsActive = true
		existing.Status = "normal" // 重新激活
		existing.ClientSecret = account.ClientSecret
		if account.Proxy != "" {
			existing.Proxy = account.Proxy
		}

		if err := database.GetDB().Save(&existing).Error; err != nil {
			c.JSON(http.StatusInternalServerError, ExternalTokenResponse{
				Success: false,
				Error:   fmt.Sprintf("更新失败: %v", err),
			})
			return
		}
		
		log.Printf("[外部API] 账号更新成功: ClientID=%s, Email=%s, Plan=%s", existing.ClientID, existing.Email, existing.PlanType)
		c.JSON(http.StatusOK, ExternalTokenResponse{
			Success: true,
			Message: "账号更新成功",
			Account: &existing,
		})
	} else {
		// 创建新账号
		if err := database.GetDB().Create(&account).Error; err != nil {
			c.JSON(http.StatusInternalServerError, ExternalTokenResponse{
				Success: false,
				Error:   fmt.Sprintf("创建失败: %v", err),
			})
			return
		}
		
		log.Printf("[外部API] 新账号创建成功: ClientID=%s, Email=%s, Plan=%s", account.ClientID, account.Email, account.PlanType)
		c.JSON(http.StatusCreated, ExternalTokenResponse{
			Success: true,
			Message: "账号创建成功",
			Account: &account,
		})
	}
}