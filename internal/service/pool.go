package service

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"zencoder2api/internal/database"
	"zencoder2api/internal/model"
)

type AccountPool struct {
	mu       sync.RWMutex
	accounts []*model.Account
	index    uint64
	maxErrs  int
	stopChan chan struct{}
}

var pool *AccountPool

func init() {
	pool = &AccountPool{
		maxErrs:  3,
		accounts: make([]*model.Account, 0),
		stopChan: make(chan struct{}),
	}
}

// InitAccountPool 初始化账号池并启动刷新协程
func InitAccountPool() {
	// 数据迁移：将旧字段状态迁移到 Status
	pool.migrateData()
	
	// 初始加载
	pool.refresh()
	// 启动后台刷新
	go pool.refreshLoop()
}

func (p *AccountPool) migrateData() {
	db := database.GetDB()
	// 默认设为 normal
	db.Model(&model.Account{}).Where("status = '' OR status IS NULL").Update("status", "normal")
	
	// 迁移冷却状态
	db.Model(&model.Account{}).Where("is_cooling = ?", true).Update("status", "cooling")
	
	// 迁移错误封禁状态
	db.Model(&model.Account{}).Where("is_active = ? AND error_count >= ?", false, p.maxErrs).Update("status", "error")
	
	// 迁移手动禁用状态 (!Active && !Cooling && Error < Max)
	db.Model(&model.Account{}).Where("is_active = ? AND is_cooling = ? AND error_count < ?", false, false, p.maxErrs).Update("status", "disabled")
	
	// 迁移 category 到 status (如果 category 是 banned/error/cooling/abnormal)
	db.Model(&model.Account{}).Where("category = ?", "banned").Update("status", "banned")
	db.Model(&model.Account{}).Where("category = ?", "error").Update("status", "error")
	db.Model(&model.Account{}).Where("category = ?", "cooling").Update("status", "cooling")
	db.Model(&model.Account{}).Where("category = ?", "abnormal").Update("status", "cooling")
}

func (p *AccountPool) refreshLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.refresh()
			p.cleanupTimeoutAccounts() // 清理超时账号
		case <-p.stopChan:
			return
		}
	}
}

// cleanupTimeoutAccounts 定期清理超时的账号状态
func (p *AccountPool) cleanupTimeoutAccounts() {
	now := time.Now()
	statusMu.Lock()
	defer statusMu.Unlock()
	
	cleanedCount := 0
	for _, status := range accountStatuses {
		// 清理超过60秒还在使用中的账号
		if status.InUse && !status.InUseSince.IsZero() && now.Sub(status.InUseSince) > 60*time.Second {
			status.InUse = false
			status.InUseSince = time.Time{}
			cleanedCount++
		}
	}
	
	if cleanedCount > 0 {
		log.Printf("[INFO] 定期清理：释放了 %d 个超时账号", cleanedCount)
	}
}

func (p *AccountPool) refresh() {
	// 先恢复冷却账号
	recoverCoolingAccounts()

	// 刷新即将过期的token（1小时内过期）
	p.refreshExpiredTokens()

	var dbAccounts []model.Account
	// 只查询状态为 normal 的账号
	result := database.GetDB().Where("status = ?", "normal").
		Where("token_expiry > ?", time.Now()).
		Find(&dbAccounts)

	if result.Error != nil {
		log.Printf("[Error] Failed to refresh account pool: %v", result.Error)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// 重新构建缓存，但保留现有对象的指针以维持状态（如果ID匹配）
	// 或者简单全量替换，依赖 30s 的一致性窗口
	// 为了简化并防止并发问题，这里使用全量替换，将 DB 数据作为 Source of Truth
	newAccounts := make([]*model.Account, len(dbAccounts))
	for i := range dbAccounts {
		newAccounts[i] = &dbAccounts[i]
	}
	
	// 如果账号数量有显著变化，记录日志
	oldCount := len(p.accounts)
	newCount := len(newAccounts)
	if oldCount != newCount {
		log.Printf("[AccountPool] 账号池刷新：%d -> %d 个可用账号", oldCount, newCount)
	}
	
	p.accounts = newAccounts
}

// refreshExpiredTokens 刷新即将过期的账号token
func (p *AccountPool) refreshExpiredTokens() {
	now := time.Now()
	threshold := now.Add(time.Hour) // 1小时内即将过期的token

	var expiredAccounts []model.Account
	// 只排除banned状态的账号，其他状态的账号仍可以刷新token
	result := database.GetDB().Where("status != ?", "banned").
		Where("client_id != '' AND client_secret != ''").
		Where("token_expiry < ?", threshold).
		Find(&expiredAccounts)

	if result.Error != nil {
		log.Printf("[AccountPool] 查询即将过期的账号失败: %v", result.Error)
		return
	}

	// 额外验证：再次过滤掉banned状态的账号
	var validAccounts []model.Account
	for _, acc := range expiredAccounts {
		if acc.Status != "banned" {
			validAccounts = append(validAccounts, acc)
		}
	}
	expiredAccounts = validAccounts

	if len(expiredAccounts) == 0 {
		return
	}

	log.Printf("[AccountPool] 发现 %d 个非封禁账号的token需要刷新", len(expiredAccounts))

	// 限制并发刷新数量，避免对API造成压力
	semaphore := make(chan struct{}, 10) // 最多10个并发
	var refreshCount int32
	var successCount int32

	// 并发刷新token
	for i := range expiredAccounts {
		account := &expiredAccounts[i]
		
		go func(acc *model.Account) {
			semaphore <- struct{}{} // 获取信号量
			defer func() { <-semaphore }() // 释放信号量
			
			refreshCount++
			
			// 根据账号类型选择不同的刷新方式
			if acc.ClientSecret == "refresh-token-login" {
				// refresh-token-login 账号使用 refresh_token 刷新
				if err := p.refreshRefreshTokenAccount(acc); err != nil {
					log.Printf("[AccountPool] refresh-token账号 %s (ID:%d) token刷新失败: %v",
						acc.ClientID, acc.ID, err)
				} else {
					successCount++
					log.Printf("[AccountPool] refresh-token账号 %s (ID:%d) token刷新成功，新过期时间: %s",
						acc.ClientID, acc.ID, acc.TokenExpiry.Format("2006-01-02 15:04:05"))
				}
			} else {
				// 普通账号使用 OAuth client credentials 刷新
				if err := p.refreshSingleAccountToken(acc); err != nil {
					log.Printf("[AccountPool] 账号 %s (ID:%d) token刷新失败: %v",
						acc.ClientID, acc.ID, err)
				} else {
					successCount++
					log.Printf("[AccountPool] 账号 %s (ID:%d) token刷新成功，新过期时间: %s",
						acc.ClientID, acc.ID, acc.TokenExpiry.Format("2006-01-02 15:04:05"))
				}
			}
		}(account)
	}

	// 等待所有刷新完成（最多等待30秒）
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			log.Printf("[AccountPool] Token刷新超时，已完成 %d/%d", refreshCount, len(expiredAccounts))
			return
		case <-ticker.C:
			if int(refreshCount) >= len(expiredAccounts) {
				log.Printf("[AccountPool] Token刷新完成：成功 %d/%d", successCount, len(expiredAccounts))
				return
			}
		}
	}
}

// refreshSingleAccountToken 刷新单个账号的token
func (p *AccountPool) refreshSingleAccountToken(account *model.Account) error {
	return refreshAccountToken(account)
}

func GetNextAccount() (*model.Account, error) {
	return GetNextAccountForModel("")
}

// AccountStatus 账号运行时状态
type AccountStatus struct {
	LastUsed       time.Time
	InUse          bool
	FrozenUntil    time.Time
	InUseSince     time.Time // 记录开始使用的时间
}

// 账号运行时状态管理
var (
	accountStatuses = make(map[uint]*AccountStatus)
	statusMu        sync.RWMutex
)

// GetNextAccountForModel 获取可用于指定模型的账号
// 使用内存状态管理，避免高并发下的竞态条件
func GetNextAccountForModel(modelID string) (*model.Account, error) {
	pool.mu.RLock()
	accounts := pool.accounts // 获取账号列表引用
	pool.mu.RUnlock()

	if len(accounts) == 0 {
		return nil, ErrNoAvailableAccount
	}

	// 获取候选账号
	var candidates []*model.Account
	now := time.Now()
	statusMu.RLock()
	for _, acc := range accounts {
		// 检查模型权限
		if modelID != "" && !model.CanUseModel(acc.PlanType, modelID) {
			continue
		}
		
		// 获取或初始化状态
		status, exists := accountStatuses[acc.ID]
		if !exists {
			// 初始化状态
			accountStatuses[acc.ID] = &AccountStatus{
				LastUsed:    acc.LastUsed,
				InUse:       false,
				FrozenUntil: acc.CoolingUntil,
				InUseSince:  time.Time{},
			}
			status = accountStatuses[acc.ID]
		}
		
		// 自动释放超时账号（超过30秒未释放的账号）
		if status.InUse && !status.InUseSince.IsZero() && now.Sub(status.InUseSince) > 30*time.Second {
			status.InUse = false
			status.InUseSince = time.Time{}
			log.Printf("[WARN] 账号 %s (ID:%d) 使用超时，已自动释放", acc.Email, acc.ID)
		}
		
		// 检查是否可用（未被使用且未被冻结）
		if !status.InUse && now.After(status.FrozenUntil) {
			candidates = append(candidates, acc)
		}
	}
	statusMu.RUnlock()

	if len(candidates) == 0 {
		// 提供详细的调试信息
		totalAccounts := len(accounts)
		inUseCount := 0
		frozenCount := 0
		noPermissionCount := 0
		
		statusMu.RLock()
		for _, acc := range accounts {
			if modelID != "" && !model.CanUseModel(acc.PlanType, modelID) {
				noPermissionCount++
				continue
			}
			
			if status, exists := accountStatuses[acc.ID]; exists {
				if status.InUse {
					inUseCount++
				} else if !now.After(status.FrozenUntil) {
					frozenCount++
				}
			}
		}
		statusMu.RUnlock()
		
		log.Printf("[ERROR] 无可用账号 - 总账号数: %d, 权限不足: %d, 使用中: %d, 冻结中: %d, 模型: %s",
			totalAccounts, noPermissionCount, inUseCount, frozenCount, modelID)
			
		return nil, ErrNoPermission
	}

	// 选择最长时间未使用的账号
	var selected *model.Account
	oldestTime := time.Now()
	
	statusMu.RLock()
	for _, acc := range candidates {
		status := accountStatuses[acc.ID]
		if status == nil {
			continue
		}
		
		// 如果账号从未使用过，优先选择
		if status.LastUsed.IsZero() {
			selected = acc
			break
		}
		// 选择最长时间未使用的账号
		if status.LastUsed.Before(oldestTime) {
			oldestTime = status.LastUsed
			selected = acc
		}
	}
	statusMu.RUnlock()
	
	// 如果没有找到合适的账号，使用轮询
	if selected == nil {
		selected = candidates[time.Now().UnixNano()%int64(len(candidates))]
	}
	
	// 立即在内存中标记账号为使用中
	statusMu.Lock()
	currentTime := time.Now()
	if status, exists := accountStatuses[selected.ID]; exists {
		status.InUse = true
		status.LastUsed = currentTime
		status.InUseSince = currentTime
	} else {
		accountStatuses[selected.ID] = &AccountStatus{
			LastUsed:    currentTime,
			InUse:       true,
			FrozenUntil: time.Time{},
			InUseSince:  currentTime,
		}
	}
	statusMu.Unlock()
	
	// 异步更新数据库
	go func(acc *model.Account, usedTime time.Time) {
		database.GetDB().Model(acc).Update("last_used", usedTime)
	}(selected, time.Now())
	
	return selected, nil
}

// ReleaseAccount 释放账号（标记为未使用）
func ReleaseAccount(account *model.Account) {
	if account == nil {
		return
	}
	
	statusMu.Lock()
	defer statusMu.Unlock()
	
	if status, exists := accountStatuses[account.ID]; exists {
		status.InUse = false
		status.InUseSince = time.Time{} // 重置使用开始时间
	}
}

// recoverCoolingAccounts 恢复冷却期已过的账号
func recoverCoolingAccounts() {
	var coolingAccounts []model.Account
	// 查询 status = cooling 且时间已到的账号（使用 UTC 时间）
	nowUTC := time.Now().UTC()
	database.GetDB().Where("status = ?", "cooling").
		Where("cooling_until < ?", nowUTC).
		Find(&coolingAccounts)

	for _, acc := range coolingAccounts {
		acc.IsCooling = false
		acc.IsActive = true
		acc.Category = "normal" // 保持兼容
		acc.Status = "normal"   // 恢复状态
		acc.BanReason = ""      // 清除封禁原因
		database.GetDB().Save(&acc)
		log.Printf("[INFO] 账号 %s (ID:%d) 冷却期结束，已恢复 (冷却结束时间: %s UTC)",
			acc.Email, acc.ID, acc.CoolingUntil.Format("2006-01-02 15:04:05"))
	}
}

func MarkAccountError(account *model.Account) {
	account.ErrorCount++
	if account.ErrorCount >= pool.maxErrs {
		account.IsActive = false
		account.Status = "error" // 更新状态
		account.Category = "error"
		account.BanReason = "Error count exceeded limit"
	}
	database.GetDB().Save(account)
}

// MarkAccountRateLimited 标记账号遇到 429 限流错误
func MarkAccountRateLimited(account *model.Account) {
	account.RateLimitHits++
	account.IsCooling = true
	account.IsActive = false

	// 设置冷却时间：1小时（使用UTC时间）
	account.CoolingUntil = time.Now().UTC().Add(1 * time.Hour)

	// 更新状态
	oldStatus := account.Status
	account.Status = "cooling"
	account.Category = "cooling"
	account.BanReason = "Rate limited (429)"

	database.GetDB().Save(account)

	log.Printf("[WARN] 账号 %s (ID:%d) 遇到 429 限流 (第 %d 次)，已移至冷却分组，冷却至 %s UTC",
		account.Email, account.ID, account.RateLimitHits, account.CoolingUntil.Format("2006-01-02 15:04:05"))

	if oldStatus != "cooling" {
		log.Printf("[INFO] 账号 %s 状态变更: %s -> cooling", account.Email, oldStatus)
	}
}

// MarkAccountRateLimitedWithResponse 根据响应头信息处理429限流错误
func MarkAccountRateLimitedWithResponse(account *model.Account, resp *http.Response) {
	if resp == nil || resp.Header == nil {
		// 如果没有响应头，使用默认处理
		MarkAccountRateLimited(account)
		return
	}
	
	// 获取响应头中的积分信息
	periodLimit := resp.Header.Get("Zen-Pricing-Period-Limit")
	periodCost := resp.Header.Get("Zen-Pricing-Period-Cost")
	periodEnd := resp.Header.Get("Zen-Pricing-Period-End")
	
	// 检查是否为积分耗尽导致的429
	isQuotaExhausted := false
	if periodLimit != "" && periodCost != "" {
		limit := parseFloat(periodLimit)
		used := parseFloat(periodCost)
		
		// 如果使用积分 >= 最大积分，说明积分已满
		if limit > 0 && used >= limit {
			isQuotaExhausted = true
		}
	}
	
	account.RateLimitHits++
	account.IsCooling = true
	account.IsActive = false
	
	oldStatus := account.Status
	account.Status = "cooling"
	account.Category = "cooling"
	
	if isQuotaExhausted {
		// 积分耗尽导致的429，根据periodEnd设置冷却时间
		if periodEnd != "" {
			if endTime, err := time.Parse(time.RFC3339, periodEnd); err == nil {
				account.CoolingUntil = endTime
				account.BanReason = "Quota exhausted (429)"
				// 同时更新积分刷新时间
				account.CreditRefreshTime = endTime
				
				log.Printf("[WARN] 账号 %s (ID:%d) 积分耗尽导致429限流，冷却至积分刷新时间: %s UTC",
					account.Email, account.ID, endTime.Format("2006-01-02 15:04:05"))
			} else {
				// 解析失败，使用默认冷却时间
				account.CoolingUntil = time.Now().UTC().Add(1 * time.Hour)
				account.BanReason = "Quota exhausted (429) - fallback cooling"
				
				log.Printf("[WARN] 账号 %s (ID:%d) 积分耗尽但无法解析刷新时间，使用默认冷却: %s UTC",
					account.Email, account.ID, account.CoolingUntil.Format("2006-01-02 15:04:05"))
			}
		} else {
			// 没有periodEnd，使用默认冷却时间
			account.CoolingUntil = time.Now().UTC().Add(1 * time.Hour)
			account.BanReason = "Quota exhausted (429) - no end time"
			
			log.Printf("[WARN] 账号 %s (ID:%d) 积分耗尽但无刷新时间信息，使用默认冷却: %s UTC",
				account.Email, account.ID, account.CoolingUntil.Format("2006-01-02 15:04:05"))
		}
	} else {
		// 常规429限流错误，使用默认冷却时间
		account.CoolingUntil = time.Now().UTC().Add(1 * time.Hour)
		account.BanReason = "Rate limited (429)"
		
		log.Printf("[WARN] 账号 %s (ID:%d) 遇到常规429限流 (第 %d 次)，冷却至: %s UTC",
			account.Email, account.ID, account.RateLimitHits, account.CoolingUntil.Format("2006-01-02 15:04:05"))
	}
	
	database.GetDB().Save(account)
	
	if oldStatus != "cooling" {
		log.Printf("[INFO] 账号 %s 状态变更: %s -> cooling", account.Email, oldStatus)
	}
}

// MarkAccountRateLimitedShort 标记账号遇到 429 限流错误（短期冷却）
func MarkAccountRateLimitedShort(account *model.Account) {
	account.RateLimitHits++
	account.IsCooling = true
	account.IsActive = false

	// 设置短期冷却时间：5秒（使用UTC时间）
	account.CoolingUntil = time.Now().UTC().Add(5 * time.Second)

	// 更新状态
	account.Status = "cooling"
	account.Category = "cooling"
	account.BanReason = "Rate limited (429) - short cooling"

	database.GetDB().Save(account)
	
	log.Printf("[INFO] 账号 %s (ID:%d) 短期冷却，冷却至 %s UTC",
		account.Email, account.ID, account.CoolingUntil.Format("2006-01-02 15:04:05"))
}

// FreezeAccount 冻结账号指定时间（用于500错误限速）
func FreezeAccount(account *model.Account, duration time.Duration) {
	if account == nil {
		return
	}
	
	freezeUntil := time.Now().Add(duration)
	
	// 立即在内存中更新冻结状态
	statusMu.Lock()
	if status, exists := accountStatuses[account.ID]; exists {
		status.FrozenUntil = freezeUntil
		status.InUse = false // 释放账号
		status.InUseSince = time.Time{} // 重置使用开始时间
	} else {
		accountStatuses[account.ID] = &AccountStatus{
			LastUsed:    time.Now(),
			InUse:       false,
			FrozenUntil: freezeUntil,
			InUseSince:  time.Time{},
		}
	}
	statusMu.Unlock()
	
	// 异步更新数据库
	go func() {
		// 设置冷却时间（使用UTC时间）
		account.CoolingUntil = freezeUntil.UTC()
		account.IsCooling = true
		account.IsActive = false
		
		// 更新状态
		account.Status = "cooling"
		account.Category = "cooling"
		account.BanReason = "Rate limit tracking problem (500)"
		
		database.GetDB().Save(account)
	}()
}

func ResetAccountError(account *model.Account) {
	account.ErrorCount = 0
	database.GetDB().Save(account)
}

// 扣减积分并检查是否需要冷却
func UseCredit(account *model.Account, multiplier float64) {
	account.DailyUsed += multiplier
	account.TotalUsed += multiplier
	account.LastUsed = time.Now()  // 更新最后使用时间

	limit := float64(model.PlanLimits[account.PlanType])
	if account.DailyUsed >= limit {
		account.IsCooling = true
		account.Status = "cooling" // 更新状态
		account.Category = "cooling"
		account.BanReason = "Daily quota exceeded"
	}

	database.GetDB().Save(account)
}

// UpdateAccountCreditsFromResponse 根据响应头中的积分信息更新账号
// 如果响应头中有积分信息，使用实际值；否则使用模型倍率
func UpdateAccountCreditsFromResponse(account *model.Account, resp *http.Response, modelMultiplier float64) {
	// 无论如何都要更新最后使用时间
	account.LastUsed = time.Now()
	
	if resp == nil || resp.Header == nil {
		// 如果没有响应头，使用模型倍率
		UseCredit(account, modelMultiplier)
		return
	}
	
	// 获取响应头中的积分信息
	periodLimit := resp.Header.Get("Zen-Pricing-Period-Limit")
	periodCost := resp.Header.Get("Zen-Pricing-Period-Cost")
	requestCost := resp.Header.Get("Zen-Request-Cost")
	periodEnd := resp.Header.Get("Zen-Pricing-Period-End")
	
	// 解析本次请求消耗的积分
	var creditUsed float64
	hasAPICredits := false
	
	if requestCost != "" {
		if val := parseFloat(requestCost); val > 0 {
			creditUsed = val
			hasAPICredits = true
		}
	}
	
	// 如果有 periodCost，更新账号的总使用量（当日总计）
	if periodCost != "" {
		if val := parseFloat(periodCost); val >= 0 {
			// 直接使用API返回的当日使用量
			account.DailyUsed = val
			hasAPICredits = true
		}
	}
	
	// 如果有 periodLimit，可以用于验证账号计划类型
	if periodLimit != "" {
		if limit := parseFloat(periodLimit); limit > 0 {
			// 可选：验证或更新账号的计划类型
			// 这里只记录日志，不改变计划类型
			expectedLimit := float64(model.PlanLimits[account.PlanType])
			if limit != expectedLimit && IsDebugMode() {
				log.Printf("[INFO] 账号 %s (ID:%d) API限额(%v)与本地限额(%v)不一致",
					account.Email, account.ID, limit, expectedLimit)
			}
		}
	}
	
	// 解析冷却到期时间（UTC时间）和积分刷新时间
	var coolingEndTime time.Time
	if periodEnd != "" {
		if t, err := time.Parse(time.RFC3339, periodEnd); err == nil {
			coolingEndTime = t
			// 同时更新积分刷新时间
			account.CreditRefreshTime = t
		} else {
			// 如果解析失败，记录日志
			log.Printf("[WARN] 无法解析 Zen-Pricing-Period-End: %s, error: %v", periodEnd, err)
		}
	}
	
	if hasAPICredits {
		// 使用API返回的积分值
		if requestCost != "" && creditUsed > 0 {
			account.TotalUsed += creditUsed
		}
		
		// 检查是否需要冷却
		limit := float64(model.PlanLimits[account.PlanType])
		if account.DailyUsed >= limit {
			account.IsCooling = true
			account.Status = "cooling"
			account.Category = "cooling"
			account.BanReason = "Daily quota exceeded"
			
			// 如果有响应头中的冷却到期时间，使用它；否则使用默认时间
			if !coolingEndTime.IsZero() {
				account.CoolingUntil = coolingEndTime
				log.Printf("[INFO] 账号 %s (ID:%d) 积分耗尽，进入冷却，到期时间: %s (UTC)",
					account.Email, account.ID, coolingEndTime.Format("2006-01-02 15:04:05"))
			} else {
				// 默认冷却到第二天的 UTC 0点
				now := time.Now().UTC()
				tomorrow := now.Add(24 * time.Hour)
				account.CoolingUntil = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)
				log.Printf("[INFO] 账号 %s (ID:%d) 积分耗尽，进入冷却至: %s (UTC)",
					account.Email, account.ID, account.CoolingUntil.Format("2006-01-02 15:04:05"))
			}
		}
		
		database.GetDB().Save(account)
		
		// 输出调试日志（仅在调试模式下）
		if IsDebugMode() && (requestCost != "" || periodCost != "") {
			log.Printf("[DEBUG] 使用API积分: 账号=%s, RequestCost=%s, PeriodCost=%s, PeriodLimit=%s, PeriodEnd=%s",
				account.Email, requestCost, periodCost, periodLimit, periodEnd)
		}
	} else {
		// 没有API积分信息，使用模型倍率（UseCredit 会自动更新 LastUsed）
		UseCredit(account, modelMultiplier)
	}
}

// parseFloat 安全地解析字符串为浮点数
func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	var val float64
	_, _ = fmt.Sscanf(s, "%f", &val)
	return val
}

// refreshRefreshTokenAccount 使用 refresh_token 刷新账号 token (用于 refresh-token-login 类型的账号)
func (p *AccountPool) refreshRefreshTokenAccount(account *model.Account) error {
	if account.RefreshToken == "" {
		return fmt.Errorf("账号 %s 缺少 refresh_token", account.ClientID)
	}

	// 调用 zencoder auth API 刷新 token
	tokenResp, err := RefreshAccessToken(account.RefreshToken, account.Proxy)
	if err != nil {
		return fmt.Errorf("调用 zencoder auth API 失败: %w", err)
	}

	// 计算过期时间
	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	// 更新数据库
	updates := map[string]interface{}{
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"token_expiry":  expiry,
		"updated_at":    time.Now(),
	}

	if err := database.GetDB().Model(&model.Account{}).
		Where("id = ?", account.ID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("更新数据库失败: %w", err)
	}

	// 更新内存中的值
	account.AccessToken = tokenResp.AccessToken
	account.RefreshToken = tokenResp.RefreshToken
	account.TokenExpiry = expiry

	return nil
}
