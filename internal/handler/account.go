package handler

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"zencoder2api/internal/database"
	"zencoder2api/internal/model"
	"zencoder2api/internal/service"
)

type AccountHandler struct{}

func NewAccountHandler() *AccountHandler {
	return &AccountHandler{}
}

func (h *AccountHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	
	// 兼容旧的 category 参数，优先使用 status
	status := c.DefaultQuery("status", "")
	if status == "" {
		category := c.DefaultQuery("category", "normal")
		if category == "abnormal" {
			status = "cooling"
		} else {
			status = category
		}
	}

	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}

	var accounts []model.Account
	var total int64

	query := database.GetDB().Model(&model.Account{})
	if status != "all" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	offset := (page - 1) * size
	if err := query.Offset(offset).Limit(size).Order("id desc").Find(&accounts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	// 调试日志：输出冷却账号的信息
	if status == "cooling" {
		for _, acc := range accounts {
			if !acc.CoolingUntil.IsZero() {
				log.Printf("[DEBUG] 冷却账号 %s (ID:%d) - CoolingUntil: %s (UTC), 现在: %s (UTC)",
					acc.Email, acc.ID,
					acc.CoolingUntil.Format("2006-01-02 15:04:05"),
					time.Now().UTC().Format("2006-01-02 15:04:05"))
			}
		}
	}

	// Calculate Stats
	var stats struct {
		TotalAccounts  int64   `json:"total_accounts"`
		NormalAccounts int64   `json:"normal_accounts"` // 原 active_accounts
		BannedAccounts int64   `json:"banned_accounts"`
		ErrorAccounts  int64   `json:"error_accounts"`
		CoolingAccounts int64  `json:"cooling_accounts"`
		DisabledAccounts int64 `json:"disabled_accounts"`
		TodayUsage     float64 `json:"today_usage"`
		TotalUsage     float64 `json:"total_usage"`
	}

	db := database.GetDB()

	db.Model(&model.Account{}).Count(&stats.TotalAccounts)
	db.Model(&model.Account{}).Where("status = ?", "normal").Count(&stats.NormalAccounts)
	db.Model(&model.Account{}).Where("status = ?", "banned").Count(&stats.BannedAccounts)
	db.Model(&model.Account{}).Where("status = ?", "error").Count(&stats.ErrorAccounts)
	db.Model(&model.Account{}).Where("status = ?", "cooling").Count(&stats.CoolingAccounts)
	db.Model(&model.Account{}).Where("status = ?", "disabled").Count(&stats.DisabledAccounts)

	db.Model(&model.Account{}).Select("COALESCE(SUM(daily_used), 0)").Scan(&stats.TodayUsage)
	db.Model(&model.Account{}).Select("COALESCE(SUM(total_used), 0)").Scan(&stats.TotalUsage)

	// 兼容前端旧字段
	statsMap := map[string]interface{}{
		"total_accounts":    stats.TotalAccounts,
		"active_accounts":   stats.NormalAccounts,
		"banned_accounts":   stats.BannedAccounts,
		"error_accounts":    stats.ErrorAccounts,
		"cooling_accounts":  stats.CoolingAccounts,
		"disabled_accounts": stats.DisabledAccounts,
		"today_usage":       stats.TodayUsage,
		"total_usage":       stats.TotalUsage,
	}

	c.JSON(http.StatusOK, gin.H{
		"items": accounts,
		"total": total,
		"page":  page,
		"size":  size,
		"stats": statsMap,
	})
}

type BatchCategoryRequest struct {
	IDs      []uint `json:"ids"`
	Category string `json:"category"` // 前端可能还传 category
	Status   string `json:"status"`
}

func (h *AccountHandler) BatchUpdateCategory(c *gin.Context) {
	var req BatchCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no ids provided"})
		return
	}

	status := req.Status
	if status == "" {
		status = req.Category
	}

	updates := map[string]interface{}{
		"status": status,
		// 兼容旧字段
		"category": status,
	}

	switch status {
	case "normal":
		updates["is_active"] = true
		updates["is_cooling"] = false
	case "cooling":
		updates["is_active"] = true // cooling 也是 active 的一种? 不，cooling 不参与轮询
		updates["is_cooling"] = true
	case "disabled":
		updates["is_active"] = false
		updates["is_cooling"] = false
	default: // banned, error
		updates["is_active"] = false
		updates["is_cooling"] = false
	}

	if err := database.GetDB().Model(&model.Account{}).Where("id IN ?", req.IDs).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 触发 refresh? 为了性能这里不触发，等待自动刷新
	c.JSON(http.StatusOK, gin.H{"message": "updated", "count": len(req.IDs)})
}

type MoveAllRequest struct {
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
}

// BatchMoveAll 一键移动某个分类的所有账号到另一个分类
func (h *AccountHandler) BatchMoveAll(c *gin.Context) {
	var req MoveAllRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.FromStatus == "" || req.ToStatus == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from_status and to_status are required"})
		return
	}

	if req.FromStatus == req.ToStatus {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from_status and to_status cannot be the same"})
		return
	}

	updates := map[string]interface{}{
		"status": req.ToStatus,
		// 兼容旧字段
		"category": req.ToStatus,
	}

	// 根据目标状态设置相应的标志
	switch req.ToStatus {
	case "normal":
		updates["is_active"] = true
		updates["is_cooling"] = false
		updates["error_count"] = 0
		updates["ban_reason"] = ""
	case "cooling":
		updates["is_active"] = false
		updates["is_cooling"] = true
		updates["ban_reason"] = ""
	case "disabled":
		updates["is_active"] = false
		updates["is_cooling"] = false
		updates["ban_reason"] = ""
	case "banned":
		updates["is_active"] = false
		updates["is_cooling"] = false
	case "error":
		updates["is_active"] = false
		updates["is_cooling"] = false
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to_status"})
		return
	}

	// 执行批量更新
	result := database.GetDB().Model(&model.Account{}).Where("status = ?", req.FromStatus).Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	log.Printf("[批量移动] 从 %s 移动到 %s，影响 %d 个账号", req.FromStatus, req.ToStatus, result.RowsAffected)

	c.JSON(http.StatusOK, gin.H{
		"message":     "moved successfully",
		"moved_count": result.RowsAffected,
		"from_status": req.FromStatus,
		"to_status":   req.ToStatus,
	})
}

type BatchRefreshTokenRequest struct {
	IDs []uint `json:"ids"`      // 选中的账号IDs，如果为空则刷新所有账号
	All bool   `json:"all"`      // 是否刷新所有账号
}

type BatchDeleteRequest struct {
	IDs       []uint `json:"ids"`       // 选中的账号IDs
	DeleteAll bool   `json:"delete_all"` // 是否删除分类中的所有账号
	Status    string `json:"status"`    // 要删除的分类状态
}

// BatchRefreshToken 批量刷新账号token
func (h *AccountHandler) BatchRefreshToken(c *gin.Context) {
	var req BatchRefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 设置流式响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "流式传输不支持"})
		return
	}

	var accounts []model.Account
	var err error

	// 根据请求类型获取要刷新的账号
	if req.All {
		// 刷新所有状态为normal且有refresh_token的账号
		err = database.GetDB().Where("status = ? AND (client_id != '' AND client_secret != '')", "normal").Find(&accounts).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取账号列表失败: " + err.Error()})
			return
		}
		log.Printf("[批量刷新Token] 准备刷新所有正常账号，共 %d 个", len(accounts))
	} else if len(req.IDs) > 0 {
		// 刷新选中的账号
		err = database.GetDB().Where("id IN ? AND (client_id != '' AND client_secret != '')", req.IDs).Find(&accounts).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取选中账号失败: " + err.Error()})
			return
		}
		log.Printf("[批量刷新Token] 准备刷新选中账号，共 %d 个", len(accounts))
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要刷新的账号或选择刷新所有账号"})
		return
	}

	if len(accounts) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有找到可刷新的账号"})
		return
	}

	// 发送开始消息
	fmt.Fprintf(c.Writer, "data: {\"type\":\"start\",\"total\":%d}\n\n", len(accounts))
	flusher.Flush()

	successCount := 0
	failCount := 0

	// 逐个刷新token
	for i, account := range accounts {
		log.Printf("[批量刷新Token] 开始刷新第 %d/%d 个账号: %s (ID:%d)", i+1, len(accounts), account.ClientID, account.ID)
		
		// 使用OAuth client credentials刷新token
		if err := service.RefreshAccountToken(&account); err != nil {
			failCount++
			errMsg := fmt.Sprintf("刷新失败: %v", err)
			
			// 检查是否是账号锁定错误
			if lockoutErr, ok := err.(*service.AccountLockoutError); ok {
				errMsg = fmt.Sprintf("账号被锁定已自动标记为封禁: %s", lockoutErr.Body)
				log.Printf("[批量刷新Token] 第 %d/%d 个账号被锁定: %s - %s", i+1, len(accounts), account.ClientID, lockoutErr.Body)
			} else {
				log.Printf("[批量刷新Token] 第 %d/%d 个账号刷新失败: %s - %v", i+1, len(accounts), account.ClientID, err)
			}
			
			fmt.Fprintf(c.Writer, "data: {\"type\":\"error\",\"index\":%d,\"account_id\":\"%s\",\"message\":\"%s\"}\n\n", i+1, account.ClientID, errMsg)
			flusher.Flush()
		} else {
			successCount++
			log.Printf("[批量刷新Token] 第 %d/%d 个账号刷新成功: %s (ID:%d)", i+1, len(accounts), account.ClientID, account.ID)
			fmt.Fprintf(c.Writer, "data: {\"type\":\"success\",\"index\":%d,\"account_id\":\"%s\",\"email\":\"%s\"}\n\n", i+1, account.ClientID, account.Email)
			flusher.Flush()
		}

		// 添加延迟避免请求过快
		if i < len(accounts)-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	// 发送完成消息
	log.Printf("[批量刷新Token] 完成: 成功 %d 个, 失败 %d 个", successCount, failCount)
	fmt.Fprintf(c.Writer, "data: {\"type\":\"complete\",\"success\":%d,\"fail\":%d}\n\n", successCount, failCount)
	flusher.Flush()
}

func (h *AccountHandler) Create(c *gin.Context) {
	var req model.AccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 生成模式 - 固定生成1个账号
	if req.GenerateMode {
		// 检查是否提供了 refresh_token 或 access_token
		if req.RefreshToken == "" && req.Token == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "生成模式需要提供 access_token 或 RefreshToken"})
			return
		}

		// 优先使用 access_token，如果同时提供了两个字段
		var masterToken string
		
		if req.Token != "" {
			// 直接使用提供的 access_token
			masterToken = req.Token
			log.Printf("[生成凭证] 使用提供的 access_token")
		} else {
			// 使用 refresh_token 获取 access_token
			tokenResp, err := service.RefreshAccessToken(req.RefreshToken, req.Proxy)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "RefreshToken 无效: " + err.Error()})
				return
			}
			masterToken = tokenResp.AccessToken
			log.Printf("[生成凭证] 通过 RefreshToken 获取了 access_token")
		}

		log.Printf("[生成凭证] 开始生成账号凭证")
		
		// 生成凭证
		cred, err := service.GenerateCredential(masterToken)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("生成失败: %v", err)})
			return
		}

		log.Printf("[生成凭证] 凭证生成成功: ClientID=%s", cred.ClientID)

		// 创建账号
		account := model.Account{
			ClientID:     cred.ClientID,
			ClientSecret: cred.Secret,
			Proxy:        req.Proxy,
			IsActive:     true,
			Status:       "normal",
		}

		// 使用生成的client_id和client_secret获取token
		// 使用OAuth client credentials方式刷新token，使用 https://fe.zencoder.ai/oauth/token
		if _, err := service.RefreshToken(&account); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("认证失败: %v", err)})
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
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("更新失败: %v", err)})
				return
			}
			
			log.Printf("[添加账号] 账号更新成功: ClientID=%s, Email=%s, Plan=%s", existing.ClientID, existing.Email, existing.PlanType)
			c.JSON(http.StatusOK, existing)
		} else {
			// 创建新账号
			if err := database.GetDB().Create(&account).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("创建失败: %v", err)})
				return
			}
			
			log.Printf("[添加账号] 新账号创建成功: ClientID=%s, Email=%s, Plan=%s", account.ClientID, account.Email, account.PlanType)
			c.JSON(http.StatusCreated, account)
		}
		return
	}

	// 原有的单个账号添加逻辑 - 现在使用 refresh_token
	account := model.Account{
		Proxy:    req.Proxy,
		IsActive: true,
		Status:   "normal",
	}

	// 优先使用 access_token，如果同时提供了两个字段则不使用 refresh_token
	if req.Token != "" && req.RefreshToken != "" {
		log.Printf("[凭证模式] 同时提供了 access_token 和 RefreshToken，优先使用 access_token")
	}
	
	if req.Token != "" {
		// JWT Parsing Logic
		payload, err := service.ParseJWT(req.Token)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的Token: " + err.Error()})
			return
		}

		account.AccessToken = req.Token
		// 优先使用ClientID字段，如果没有则使用Subject
		if payload.ClientID != "" {
			account.ClientID = payload.ClientID
		} else {
			account.ClientID = payload.Subject
		}

		account.Email = payload.Email
		account.SubscriptionStartDate = service.GetSubscriptionDate(payload)

		if payload.Expiration > 0 {
			account.TokenExpiry = time.Unix(payload.Expiration, 0)
		} else {
			account.TokenExpiry = time.Now().Add(24 * time.Hour) // 默认24小时
		}

		// Map PlanType
		plan := payload.CustomClaims.Plan
		
		// Simple normalization
		if plan != "" {
			plan = strings.ToUpper(plan[:1]) + plan[1:]
		}
		account.PlanType = model.PlanType(plan)
		if account.PlanType == "" {
			account.PlanType = model.PlanFree
		}

		// Placeholder for secret since it's required by DB but not in JWT
		account.ClientSecret = "jwt-login"
	} else if req.RefreshToken != "" {
		// 只提供了 refresh_token，使用它来获取 access_token
		tokenResp, err := service.RefreshAccessToken(req.RefreshToken, req.Proxy)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "RefreshToken 无效: " + err.Error()})
			return
		}

		account.AccessToken = tokenResp.AccessToken
		account.RefreshToken = tokenResp.RefreshToken
		account.TokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

		// 解析 Token 获取详细信息
		if payload, err := service.ParseJWT(tokenResp.AccessToken); err == nil {
			// 设置 Email
			if payload.Email != "" {
				account.Email = payload.Email
			} else if tokenResp.Email != "" {
				account.Email = tokenResp.Email
			}
			
			// 设置 ClientID - 优先使用 Email 作为唯一标识符
			if payload.Email != "" {
				account.ClientID = payload.Email
			} else if payload.Subject != "" {
				account.ClientID = payload.Subject
			} else if payload.ClientID != "" {
				account.ClientID = payload.ClientID
			}
			
			account.SubscriptionStartDate = service.GetSubscriptionDate(payload)

			// Map PlanType
			plan := payload.CustomClaims.Plan
			if plan != "" {
				plan = strings.ToUpper(plan[:1]) + plan[1:]
			}
			account.PlanType = model.PlanType(plan)
			if account.PlanType == "" {
				account.PlanType = model.PlanFree
			}
			
			log.Printf("[凭证模式-RefreshToken] 解析JWT成功: ClientID=%s, Email=%s, Plan=%s",
				account.ClientID, account.Email, account.PlanType)
		} else {
			log.Printf("[凭证模式-RefreshToken] 解析JWT失败: %v", err)
			// 如果JWT解析失败，使用 tokenResp 中的信息
			if tokenResp.UserID != "" {
				account.ClientID = tokenResp.UserID
				account.Email = tokenResp.UserID
			}
		}

		// 生成一个占位 ClientSecret
		account.ClientSecret = "refresh-token-login"
		
		// 确保 ClientID 不为空
		if account.ClientID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无法获取用户信息，请检查RefreshToken是否有效"})
			return
		}
		
	} else {
		// Old Logic
		if req.PlanType == "" {
			req.PlanType = model.PlanFree
		}
		account.ClientID = req.ClientID
		account.ClientSecret = req.ClientSecret
		account.Email = req.Email
		account.PlanType = req.PlanType

		// 验证Token是否能正确获取
		if _, err := service.RefreshToken(&account); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "认证失败: " + err.Error()})
			return
		}

		// 解析Token获取详细信息
		if payload, err := service.ParseJWT(account.AccessToken); err == nil {
			if account.Email == "" {
				account.Email = payload.Email
			}
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
	}

	// Check if account exists - 使用 Count 避免 record not found 警告
	var existing model.Account
	var count int64
	database.GetDB().Model(&model.Account{}).Where("client_id = ?", account.ClientID).Count(&count)
	if count > 0 {
		// 获取现有账号
		database.GetDB().Where("client_id = ?", account.ClientID).First(&existing)
		// Update existing
		existing.AccessToken = account.AccessToken
		existing.RefreshToken = account.RefreshToken // 更新 refresh_token
		existing.TokenExpiry = account.TokenExpiry
		existing.PlanType = account.PlanType
		existing.Email = account.Email
		existing.SubscriptionStartDate = account.SubscriptionStartDate
		existing.IsActive = true
		existing.Status = "normal"
		if account.Proxy != "" {
			existing.Proxy = account.Proxy
		}
		// If secret was provided manually, update it. If placeholder, keep existing.
		if account.ClientSecret != "jwt-login" && account.ClientSecret != "refresh-token-login" {
			existing.ClientSecret = account.ClientSecret
		}

		if err := database.GetDB().Save(&existing).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, existing)
		return
	}

	if err := database.GetDB().Create(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, account)
}

func (h *AccountHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var account model.Account
	if err := database.GetDB().First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	var req model.AccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	account.Email = req.Email
	account.PlanType = req.PlanType
	account.Proxy = req.Proxy

	if err := database.GetDB().Save(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, account)
}

// BatchDelete 批量删除账号
func (h *AccountHandler) BatchDelete(c *gin.Context) {
	var req BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var deletedCount int64

	if req.DeleteAll {
		// 删除指定分类的所有账号
		if req.Status == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "delete_all模式需要指定status"})
			return
		}

		// 执行删除操作
		result := database.GetDB().Where("status = ?", req.Status).Delete(&model.Account{})
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
			return
		}

		deletedCount = result.RowsAffected
		log.Printf("[批量删除] 删除分类 %s 的所有账号，共删除 %d 个", req.Status, deletedCount)

	} else {
		// 删除选中的账号
		if len(req.IDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "未选择要删除的账号"})
			return
		}

		// 执行删除操作
		result := database.GetDB().Where("id IN ?", req.IDs).Delete(&model.Account{})
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
			return
		}

		deletedCount = result.RowsAffected
		log.Printf("[批量删除] 删除选中的 %d 个账号，实际删除 %d 个", len(req.IDs), deletedCount)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "批量删除成功",
		"deleted_count": deletedCount,
	})
}

func (h *AccountHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := database.GetDB().Delete(&model.Account{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *AccountHandler) Toggle(c *gin.Context) {
	id := c.Param("id")
	var account model.Account
	if err := database.GetDB().First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}

	// 切换 Disabled / Normal
	if account.Status == "disabled" || !account.IsActive {
		account.Status = "normal"
		account.IsActive = true
		account.IsCooling = false
		account.ErrorCount = 0
	} else {
		account.Status = "disabled"
		account.IsActive = false
	}
	
	database.GetDB().Save(&account)

	c.JSON(http.StatusOK, account)
}
