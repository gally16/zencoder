package service

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"zencoder2api/internal/database"
	"zencoder2api/internal/model"
	
	"gorm.io/gorm"
)

type AutoGenerationService struct {
	mu              sync.Mutex
	lastTriggered   map[uint]time.Time // tokenID -> last triggered time
	isGenerating    map[uint]bool      // tokenID -> is generating
	debounceTime    time.Duration      // 防抖时间
	generationDelay time.Duration      // 生成任务间隔时间
}

var autoGenService *AutoGenerationService

func InitAutoGenerationService() {
	autoGenService = &AutoGenerationService{
		lastTriggered:   make(map[uint]time.Time),
		isGenerating:    make(map[uint]bool),
		debounceTime:    5 * time.Minute,  // 5分钟防抖
		generationDelay: 1 * time.Hour,    // 生成任务间隔1小时
	}
	
	// 启动监控协程
	go autoGenService.startMonitoring()
}

// SaveGenerationToken 保存生成模式使用的token
func SaveGenerationToken(token string, description string) error {
	db := database.GetDB()
	
	// 检查是否已存在
	var existing model.TokenRecord
	if err := db.Where("token = ?", token).First(&existing).Error; err == nil {
		// 更新最后生成时间
		existing.LastGeneratedAt = time.Now()
		existing.GeneratedCount += 1
		return db.Save(&existing).Error
	}
	
	// 创建新记录
	record := model.TokenRecord{
		Token:           token,
		Description:     description,
		GeneratedCount:  1,
		LastGeneratedAt: time.Now(),
		AutoGenerate:    true,
		Threshold:       10,
		GenerateBatch:   30,
		IsActive:        true,
	}
	
	return db.Create(&record).Error
}

// SaveGenerationTokenWithRefresh 保存生成模式使用的 refresh_token
func SaveGenerationTokenWithRefresh(refreshToken string, accessToken string, description string, expiresIn int) error {
	db := database.GetDB()

	// 计算过期时间
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	// 解析JWT获取用户信息，特别是邮箱
	var email, planType string
	var subscriptionDate time.Time
	
	if accessToken != "" {
		if payload, err := ParseJWT(accessToken); err == nil {
			email = payload.Email
			planType = payload.CustomClaims.Plan
			if planType != "" {
				planType = strings.ToUpper(planType[:1]) + planType[1:]
			}
			subscriptionDate = GetSubscriptionDate(payload)
			log.Printf("[SaveGenerationToken] 解析JWT成功: Email=%s, Plan=%s, SubStart=%s",
				email, planType, subscriptionDate.Format("2006-01-02"))
		} else {
			log.Printf("[SaveGenerationToken] 解析JWT失败: %v", err)
		}
	}

	// 如果有邮箱，按邮箱查找；否则按refresh_token查找
	var existing model.TokenRecord
	var err error
	
	if email != "" {
		// 优先按邮箱查找，实现相同邮箱的记录合并
		err = db.Where("email = ?", email).First(&existing).Error
	} else {
		// 没有邮箱时，按refresh_token查找
		err = db.Where("refresh_token = ?", refreshToken).First(&existing).Error
	}

	if err == nil {
		// 更新现有记录
		updates := map[string]interface{}{
			"token":                   accessToken,
			"refresh_token":          refreshToken,
			"token_expiry":           expiresAt,
			"description":            description,
			"updated_at":             time.Now(),
			"plan_type":              planType,
			"subscription_start_date": subscriptionDate,
		}
		
		// 如果之前没有refresh_token，标记为有
		if existing.RefreshToken == "" {
			updates["has_refresh_token"] = true
		}
		
		return db.Model(&existing).Updates(updates).Error
	}

	// 创建新记录
	record := model.TokenRecord{
		Token:                 accessToken,
		RefreshToken:         refreshToken,
		TokenExpiry:          expiresAt,
		Description:          description,
		Email:                email,
		PlanType:             planType,
		SubscriptionStartDate: subscriptionDate,
		HasRefreshToken:      true,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
		AutoGenerate:         true,
		Threshold:            10,
		GenerateBatch:        30,
		IsActive:             true,
		GeneratedCount:       0,
		TotalSuccess:         0,
		TotalFail:            0,
	}

	if err := db.Create(&record).Error; err != nil {
		return fmt.Errorf("failed to save generation token: %w", err)
	}

	return nil
}

// GetActiveTokenRecords 获取所有活跃的token记录
func GetActiveTokenRecords() ([]model.TokenRecord, error) {
	var records []model.TokenRecord
	err := database.GetDB().Where("is_active = ?", true).Find(&records).Error
	return records, err
}

// GetAllTokenRecords 获取所有token记录
func GetAllTokenRecords() ([]model.TokenRecord, error) {
	var records []model.TokenRecord
	err := database.GetDB().Order("created_at DESC").Find(&records).Error
	return records, err
}

// GetGenerationTasks 获取生成任务历史
func GetGenerationTasks(tokenRecordID uint) ([]model.GenerationTask, error) {
	var tasks []model.GenerationTask
	query := database.GetDB().Order("created_at DESC")
	if tokenRecordID > 0 {
		query = query.Where("token_record_id = ?", tokenRecordID)
	}
	err := query.Find(&tasks).Error
	return tasks, err
}

// UpdateTokenRecord 更新token记录设置
func UpdateTokenRecord(id uint, updates map[string]interface{}) error {
	return database.GetDB().Model(&model.TokenRecord{}).Where("id = ?", id).Updates(updates).Error
}

// 监控账号池并触发自动生成
func (s *AutoGenerationService) startMonitoring() {
	ticker := time.NewTicker(1 * time.Minute) // 每分钟检查一次
	defer ticker.Stop()
	
	for range ticker.C {
		s.checkAndTriggerGeneration()
	}
}

// 检查并触发生成
func (s *AutoGenerationService) checkAndTriggerGeneration() {
	// 获取所有活跃的token记录
	records, err := GetActiveTokenRecords()
	if err != nil {
		log.Printf("[AutoGen] 获取token记录失败: %v", err)
		return
	}
	
	// 计算当前可用账号数量
	var activeAccountCount int64
	database.GetDB().Model(&model.Account{}).
		Where("status = ?", "normal").
		Where("token_expiry > ?", time.Now()).
		Count(&activeAccountCount)
	
	log.Printf("[AutoGen] 当前活跃账号数量: %d", activeAccountCount)
	
	// 检查每个token记录的阈值
	for _, record := range records {
		if !record.AutoGenerate || !record.IsActive {
			continue
		}
		
		// 检查是否达到阈值
		if int(activeAccountCount) <= record.Threshold {
			s.triggerGeneration(record)
		}
	}
}

// 触发生成任务（带防抖）
func (s *AutoGenerationService) triggerGeneration(record model.TokenRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// 检查是否正在生成
	if s.isGenerating[record.ID] {
		log.Printf("[AutoGen] Token %d 正在生成中，跳过", record.ID)
		return
	}
	
	// 检查防抖时间
	if lastTime, ok := s.lastTriggered[record.ID]; ok {
		if time.Since(lastTime) < s.debounceTime {
			log.Printf("[AutoGen] Token %d 防抖中，距上次触发 %v", record.ID, time.Since(lastTime))
			return
		}
	}
	
	// 检查生成间隔
	if !record.LastGeneratedAt.IsZero() && time.Since(record.LastGeneratedAt) < s.generationDelay {
		log.Printf("[AutoGen] Token %d 未达到生成间隔时间，距上次生成 %v", record.ID, time.Since(record.LastGeneratedAt))
		return
	}
	
	// 标记开始生成
	s.isGenerating[record.ID] = true
	s.lastTriggered[record.ID] = time.Now()
	
	// 异步执行生成任务
	go s.executeGeneration(record)
}

// 执行生成任务
func (s *AutoGenerationService) executeGeneration(record model.TokenRecord) {
	defer func() {
		s.mu.Lock()
		s.isGenerating[record.ID] = false
		s.mu.Unlock()
	}()
	
	log.Printf("[AutoGen] 开始自动生成任务 - Token %d, 批次大小: %d", record.ID, record.GenerateBatch)
	
	// 检查token记录状态
	if record.Status != "active" {
		log.Printf("[AutoGen] Token记录 %d 状态异常 (%s)，跳过生成任务", record.ID, record.Status)
		return
	}
	
	// 检查token是否需要刷新
	if record.RefreshToken != "" && time.Now().After(record.TokenExpiry.Add(-time.Hour)) {
		log.Printf("[AutoGen] Token记录 %d 的token即将过期，尝试刷新", record.ID)
		if err := UpdateTokenRecordToken(&record); err != nil {
			log.Printf("[AutoGen] Token记录 %d 刷新失败，停止生成任务: %v", record.ID, err)
			return
		}
	}
	
	// 创建生成任务记录
	task := model.GenerationTask{
		TokenRecordID: record.ID,
		Token:         record.Token,
		BatchSize:     record.GenerateBatch,
		Status:        "running",
		StartedAt:     time.Now(),
	}
	
	if err := database.GetDB().Create(&task).Error; err != nil {
		log.Printf("[AutoGen] 创建任务记录失败: %v", err)
		return
	}
	
	// 批量生成凭证
	credentials, errs := BatchGenerateCredentials(record.Token, record.GenerateBatch)
	
	// 检查生成过程中是否有token失效的错误
	for _, err := range errs {
		if strings.Contains(err.Error(), "locked out") || strings.Contains(err.Error(), "User is locked out") {
			log.Printf("[AutoGen] 检测到原始token被锁定，禁用token记录 %d: %v", record.ID, err)
			// 将token记录标记为封禁状态
			if markErr := markTokenRecordAsBanned(&record, "原始token被锁定: "+err.Error()); markErr != nil {
				log.Printf("[AutoGen] 标记token记录封禁状态失败: %v", markErr)
			}
			// 根据邮箱禁用相关的token记录
			if record.Email != "" {
				if disableErr := disableTokenRecordsByEmail(record.Email, "关联账号被锁定"); disableErr != nil {
					log.Printf("[AutoGen] 禁用相关token记录失败: %v", disableErr)
				}
			}
			// 提前结束任务
			task.Status = "failed"
			task.ErrorMessage = "原始token被锁定"
			task.CompletedAt = time.Now()
			database.GetDB().Save(&task)
			return
		}
	}
	
	successCount := 0
	failCount := len(errs)
	
	// 处理生成的凭证
	for _, cred := range credentials {
		account := model.Account{
			ClientID:     cred.ClientID,
			ClientSecret: cred.Secret,
			IsActive:     true,
			Status:       "normal",
		}
		
		// 获取Token并解析信息
		if _, err := RefreshToken(&account); err != nil {
			failCount++
			// 检查是否是账号锁定错误
			if lockoutErr, ok := err.(*AccountLockoutError); ok {
				log.Printf("[AutoGen] 账号 %s 被锁定: %s", cred.ClientID, lockoutErr.Body)
			} else {
				log.Printf("[AutoGen] 账号 %s 认证失败: %v", cred.ClientID, err)
			}
			continue
		}
		
		// 解析JWT获取详细信息
		if payload, err := ParseJWT(account.AccessToken); err == nil {
			account.Email = payload.Email
			account.SubscriptionStartDate = GetSubscriptionDate(payload)
			
			if payload.Expiration > 0 {
				account.TokenExpiry = time.Unix(payload.Expiration, 0)
			}
			
			// 设置计划类型
			plan := "Free"
			if payload.CustomClaims.Plan != "" {
				plan = payload.CustomClaims.Plan
			}
			account.PlanType = model.PlanType(plan)
		}
		
		// 保存账号
		var existing model.Account
		err := database.GetDB().Where("client_id = ?", account.ClientID).First(&existing).Error
		
		if err == nil {
			// 更新已存在的账号
			existing.AccessToken = account.AccessToken
			existing.TokenExpiry = account.TokenExpiry
			existing.PlanType = account.PlanType
			existing.Email = account.Email
			existing.SubscriptionStartDate = account.SubscriptionStartDate
			existing.IsActive = true
			existing.Status = "normal"
			existing.ClientSecret = account.ClientSecret
			
			if err := database.GetDB().Save(&existing).Error; err != nil {
				failCount++
				log.Printf("[AutoGen] 更新账号 %s 失败: %v", account.ClientID, err)
			} else {
				successCount++
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			// 记录不存在是正常的，创建新账号（不输出错误日志）
			if err := database.GetDB().Create(&account).Error; err != nil {
				failCount++
				log.Printf("[AutoGen] 创建账号 %s 失败: %v", account.ClientID, err)
			} else {
				successCount++
			}
		} else {
			// 其他数据库错误（非record not found的真实错误）
			failCount++
			log.Printf("[AutoGen] 查询账号 %s 时发生数据库错误: %v", account.ClientID, err)
		}
	}
	
	// 更新任务状态
	task.SuccessCount = successCount
	task.FailCount = failCount
	task.Status = "completed"
	if successCount == 0 && failCount > 0 {
		task.Status = "failed"
		task.ErrorMessage = fmt.Sprintf("所有账号生成失败")
	}
	task.CompletedAt = time.Now()
	
	if err := database.GetDB().Save(&task).Error; err != nil {
		log.Printf("[AutoGen] 更新任务记录失败: %v", err)
	}
	
	// 更新token记录，累计所有统计数据
	updates := map[string]interface{}{
		"last_generated_at": time.Now(),
		"generated_count":   gorm.Expr("generated_count + ?", successCount),
		"total_success":     gorm.Expr("total_success + ?", successCount),
		"total_fail":        gorm.Expr("total_fail + ?", failCount),
		"total_tasks":       gorm.Expr("total_tasks + 1"),
	}
	
	if err := database.GetDB().Model(&model.TokenRecord{}).
		Where("id = ?", record.ID).
		Updates(updates).Error; err != nil {
		log.Printf("[AutoGen] 更新token记录失败: %v", err)
	}
	
	// 刷新账号池
	RefreshAccountPool()
	
	log.Printf("[AutoGen] 自动生成完成 - 成功: %d, 失败: %d", successCount, failCount)
}

// ManualTriggerGeneration 手动触发生成
func ManualTriggerGeneration(tokenRecordID uint) error {
	var record model.TokenRecord
	if err := database.GetDB().First(&record, tokenRecordID).Error; err != nil {
		return fmt.Errorf("token记录不存在: %v", err)
	}
	
	if !record.IsActive {
		return fmt.Errorf("token记录未激活")
	}
	
	go autoGenService.executeGeneration(record)
	return nil
}

// RefreshAccountPool 刷新账号池
func RefreshAccountPool() {
	if pool != nil {
		pool.refresh()
	}
}