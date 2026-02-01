package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"zencoder2api/internal/database"
	"zencoder2api/internal/model"
	"zencoder2api/internal/service"
)

type TokenHandler struct{}

func NewTokenHandler() *TokenHandler {
	return &TokenHandler{}
}

// ListTokenRecords 获取所有token记录
func (h *TokenHandler) ListTokenRecords(c *gin.Context) {
	records, err := service.GetAllTokenRecords()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 获取每个token的生成任务统计
	var enrichedRecords []map[string]interface{}
	for _, record := range records {
		// 统计该token的任务信息
		var taskStats struct {
			TotalTasks    int64 `json:"total_tasks"`
			TotalSuccess  int64 `json:"total_success"`
			TotalFail     int64 `json:"total_fail"`
			RunningTasks  int64 `json:"running_tasks"`
		}
		
		db := database.GetDB()
		db.Model(&model.GenerationTask{}).Where("token_record_id = ?", record.ID).Count(&taskStats.TotalTasks)
		db.Model(&model.GenerationTask{}).Where("token_record_id = ?", record.ID).
			Select("COALESCE(SUM(success_count), 0)").Scan(&taskStats.TotalSuccess)
		db.Model(&model.GenerationTask{}).Where("token_record_id = ?", record.ID).
			Select("COALESCE(SUM(fail_count), 0)").Scan(&taskStats.TotalFail)
		db.Model(&model.GenerationTask{}).Where("token_record_id = ? AND status = ?", record.ID, "running").
			Count(&taskStats.RunningTasks)

		// 解析JWT获取用户信息
		var email string
		var planType string
		var subscriptionStartDate time.Time
		if record.Token != "" {
			if payload, err := service.ParseJWT(record.Token); err == nil {
				email = payload.Email
				planType = payload.CustomClaims.Plan
				if planType != "" {
					planType = strings.ToUpper(planType[:1]) + planType[1:]
				}
				// 获取订阅开始时间
				subscriptionStartDate = service.GetSubscriptionDate(payload)
			}
		}

		enrichedRecord := map[string]interface{}{
			"id":                      record.ID,
			"description":             record.Description,
			"generated_count":         record.GeneratedCount,
			"last_generated_at":       record.LastGeneratedAt,
			"auto_generate":           record.AutoGenerate,
			"threshold":               record.Threshold,
			"generate_batch":          record.GenerateBatch,
			"is_active":               record.IsActive,
			"created_at":              record.CreatedAt,
			"updated_at":              record.UpdatedAt,
			"token_expiry":            record.TokenExpiry,
			"status":                  record.Status,
			"ban_reason":              record.BanReason,
			"email":                   email,
			"plan_type":               planType,
			"subscription_start_date": subscriptionStartDate,
			"has_refresh_token":       record.RefreshToken != "",
			"total_tasks":             taskStats.TotalTasks,
			"total_success":           taskStats.TotalSuccess,
			"total_fail":              taskStats.TotalFail,
			"running_tasks":           taskStats.RunningTasks,
		}
		enrichedRecords = append(enrichedRecords, enrichedRecord)
	}

	c.JSON(http.StatusOK, gin.H{
		"items": enrichedRecords,
		"total": len(enrichedRecords),
	})
}

// UpdateTokenRecord 更新token记录配置
func (h *TokenHandler) UpdateTokenRecord(c *gin.Context) {
	id := c.Param("id")
	tokenID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req struct {
		AutoGenerate  *bool `json:"auto_generate"`
		Threshold     *int  `json:"threshold"`
		GenerateBatch *int  `json:"generate_batch"`
		IsActive      *bool `json:"is_active"`
		Description   string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := make(map[string]interface{})
	if req.AutoGenerate != nil {
		updates["auto_generate"] = *req.AutoGenerate
	}
	if req.Threshold != nil {
		updates["threshold"] = *req.Threshold
	}
	if req.GenerateBatch != nil {
		updates["generate_batch"] = *req.GenerateBatch
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}

	if err := service.UpdateTokenRecord(uint(tokenID), updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "updated"})
}

// GetGenerationTasks 获取生成任务历史
func (h *TokenHandler) GetGenerationTasks(c *gin.Context) {
	tokenRecordID := c.Query("token_record_id")
	var tokenID uint
	if tokenRecordID != "" {
		id, err := strconv.ParseUint(tokenRecordID, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token_record_id"})
			return
		}
		tokenID = uint(id)
	}

	tasks, err := service.GetGenerationTasks(tokenID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": tasks,
		"total": len(tasks),
	})
}

// TriggerGeneration 手动触发生成
func (h *TokenHandler) TriggerGeneration(c *gin.Context) {
	id := c.Param("id")
	tokenID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	if err := service.ManualTriggerGeneration(uint(tokenID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "生成任务已触发"})
}

// GetPoolStatus 获取号池状态
func (h *TokenHandler) GetPoolStatus(c *gin.Context) {
	db := database.GetDB()
	
	var stats struct {
		TotalAccounts   int64 `json:"total_accounts"`
		NormalAccounts  int64 `json:"normal_accounts"`
		CoolingAccounts int64 `json:"cooling_accounts"`
		BannedAccounts  int64 `json:"banned_accounts"`
		ErrorAccounts   int64 `json:"error_accounts"`
		DisabledAccounts int64 `json:"disabled_accounts"`
		ActiveTokens    int64 `json:"active_tokens"`
		RunningTasks    int64 `json:"running_tasks"`
	}

	// 统计账号状态
	db.Model(&model.Account{}).Count(&stats.TotalAccounts)
	db.Model(&model.Account{}).Where("status = ?", "normal").Count(&stats.NormalAccounts)
	db.Model(&model.Account{}).Where("status = ?", "cooling").Count(&stats.CoolingAccounts)
	db.Model(&model.Account{}).Where("status = ?", "banned").Count(&stats.BannedAccounts)
	db.Model(&model.Account{}).Where("status = ?", "error").Count(&stats.ErrorAccounts)
	db.Model(&model.Account{}).Where("status = ?", "disabled").Count(&stats.DisabledAccounts)
	
	// 统计激活的token
	db.Model(&model.TokenRecord{}).Where("is_active = ?", true).Count(&stats.ActiveTokens)
	
	// 统计运行中的任务
	db.Model(&model.GenerationTask{}).Where("status = ?", "running").Count(&stats.RunningTasks)

	c.JSON(http.StatusOK, stats)
}

// DeleteTokenRecord 删除token记录
func (h *TokenHandler) DeleteTokenRecord(c *gin.Context) {
	id := c.Param("id")
	tokenID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	// 开启事务，确保删除操作的原子性
	db := database.GetDB()
	tx := db.Begin()
	
	// 先删除所有关联的生成任务历史记录
	if err := tx.Where("token_record_id = ?", tokenID).Delete(&model.GenerationTask{}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除关联任务失败: " + err.Error()})
		return
	}
	
	// 删除token记录本身
	if err := tx.Delete(&model.TokenRecord{}, tokenID).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除token记录失败: " + err.Error()})
		return
	}
	
	// 提交事务
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交事务失败: " + err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"message": "token及其所有历史记录已删除"})
}

// RefreshTokenRecord 刷新token记录
func (h *TokenHandler) RefreshTokenRecord(c *gin.Context) {
	id := c.Param("id")
	tokenID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	// 调用service层的刷新函数
	if err := service.RefreshTokenAndAccounts(uint(tokenID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Token刷新成功，相关账号刷新已启动"})
}