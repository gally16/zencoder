package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"zencoder2api/internal/model"
	"zencoder2api/internal/database"
	"zencoder2api/internal/service/provider"
)

// RefreshTokenRequest 请求刷新token的结构
type RefreshTokenRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
}

// RefreshTokenResponse 刷新token的响应结构
type RefreshTokenResponse struct {
	TokenType      string                 `json:"token_type"`
	AccessToken    string                 `json:"access_token"`
	IDToken        string                 `json:"id_token"`
	RefreshToken   string                 `json:"refresh_token"`
	ExpiresIn      int                    `json:"expires_in"`
	Federated      map[string]interface{} `json:"federated"`
	
	// 这些字段可能不在响应中，但我们可以从JWT解析
	UserID         string `json:"-"`
	Email          string `json:"-"`
}

// AccountLockoutError 表示账号被锁定的错误
type AccountLockoutError struct {
	StatusCode int
	Body       string
	AccountID  string
}

func (e *AccountLockoutError) Error() string {
	return fmt.Sprintf("account %s is locked out: status %d, body: %s", e.AccountID, e.StatusCode, e.Body)
}

// isAccountLockoutError 检查是否是账号锁定错误
func isAccountLockoutError(statusCode int, body string) bool {
	if statusCode == 400 {
		// 检查响应体中是否包含锁定信息
		return strings.Contains(body, "User is locked out") ||
		       strings.Contains(body, "user is locked out") ||
		       strings.Contains(body, "locked out")
	}
	return false
}

// markAccountAsBanned 将账号标记为被封禁状态
func markAccountAsBanned(account *model.Account, reason string) error {
	updates := map[string]interface{}{
		"status":     "banned",
		"is_active":  false,
		"is_cooling": false,
		"ban_reason": reason,
		"updated_at": time.Now(),
	}
	
	if err := database.GetDB().Model(&model.Account{}).
		Where("id = ?", account.ID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update account status: %w", err)
	}
	
	log.Printf("[账号管理] 账号 %s (ID:%d) 已标记为封禁状态: %s", account.ClientID, account.ID, reason)
	return nil
}

// isRefreshTokenInvalidError 检查是否是refresh token无效错误
func isRefreshTokenInvalidError(statusCode int, body string) bool {
	if statusCode == 401 {
		return strings.Contains(body, "Refresh token is not valid") ||
		       strings.Contains(body, "refresh token is not valid") ||
		       strings.Contains(body, "invalid refresh token") ||
		       strings.Contains(body, "refresh_token is invalid")
	}
	return false
}

// markTokenRecordAsBanned 将token记录标记为封禁状态
func markTokenRecordAsBanned(record *model.TokenRecord, reason string) error {
	updates := map[string]interface{}{
		"status":      "banned",
		"is_active":   false,
		"ban_reason":  reason,
		"updated_at":  time.Now(),
	}
	
	if err := database.GetDB().Model(&model.TokenRecord{}).
		Where("id = ?", record.ID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update token record status: %w", err)
	}
	
	log.Printf("[Token管理] Token记录 #%d 已标记为封禁状态: %s", record.ID, reason)
	return nil
}

// markTokenRecordAsExpired 将token记录标记为过期状态
func markTokenRecordAsExpired(record *model.TokenRecord, reason string) error {
	updates := map[string]interface{}{
		"status":      "expired",
		"is_active":   false,
		"ban_reason":  reason,
		"updated_at":  time.Now(),
	}
	
	if err := database.GetDB().Model(&model.TokenRecord{}).
		Where("id = ?", record.ID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update token record status: %w", err)
	}
	
	log.Printf("[Token管理] Token记录 #%d 已标记为过期状态: %s", record.ID, reason)
	return nil
}

// disableTokenRecordsByEmail 根据邮箱禁用相关的token记录
func disableTokenRecordsByEmail(email string, reason string) error {
	updates := map[string]interface{}{
		"status":      "banned",
		"is_active":   false,
		"ban_reason":  reason,
		"updated_at":  time.Now(),
	}
	
	result := database.GetDB().Model(&model.TokenRecord{}).
		Where("email = ? AND status = ?", email, "active").
		Updates(updates)
	
	if result.Error != nil {
		return fmt.Errorf("failed to disable token records: %w", result.Error)
	}
	
	if result.RowsAffected > 0 {
		log.Printf("[Token管理] 已禁用邮箱 %s 相关的 %d 条token记录: %s", email, result.RowsAffected, reason)
	}
	
	return nil
}

// RefreshAccessToken 使用 refresh_token 获取新的 access_token
func RefreshAccessToken(refreshToken string, proxy string) (*RefreshTokenResponse, error) {
	url := "https://auth.zencoder.ai/api/frontegg/oauth/token"
	
	// 打印调试日志
	if IsDebugMode() {
		log.Printf("[DEBUG] [RefreshToken] >>> 开始刷新Token")
		log.Printf("[DEBUG] [RefreshToken] 请求URL: %s", url)
		if len(refreshToken) > 20 {
			log.Printf("[DEBUG] [RefreshToken] RefreshToken: %s...", refreshToken[:20])
		} else {
			log.Printf("[DEBUG] [RefreshToken] RefreshToken: %s", refreshToken)
		}
	}
	
	reqBody := RefreshTokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
	}
	
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	if IsDebugMode() {
		log.Printf("[DEBUG] [RefreshToken] 请求Body: %s", string(jsonData))
	}
	
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// 设置请求头
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8,zh-TW;q=0.7,ja;q=0.6")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://auth.zencoder.ai")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Priority", "u=1, i")
	req.Header.Set("Sec-Ch-Ua", `"Google Chrome";v="143", "Chromium";v="143", "Not A(Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36")
	req.Header.Set("X-Frontegg-Framework", "react@18.2.0")
	req.Header.Set("X-Frontegg-Sdk", "@frontegg/react@7.12.14")
	
	// 使用客户端执行请求
	client := provider.NewHTTPClient(proxy, 30*time.Second)
	
	if IsDebugMode() {
		log.Printf("[DEBUG] [RefreshToken] → 发送请求...")
	}
	
	resp, err := client.Do(req)
	if err != nil {
		if IsDebugMode() {
			log.Printf("[DEBUG] [RefreshToken] ✗ 请求失败: %v", err)
		}
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	if IsDebugMode() {
		log.Printf("[DEBUG] [RefreshToken] ← 收到响应: status=%d", resp.StatusCode)
		// 输出响应头
		log.Printf("[DEBUG] [RefreshToken] 响应头:")
		for k, v := range resp.Header {
			log.Printf("[DEBUG] [RefreshToken]   %s: %v", k, v)
		}
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	if IsDebugMode() {
		log.Printf("[DEBUG] [RefreshToken] 响应Body: %s", string(body))
	}
	
	if resp.StatusCode != http.StatusOK {
		if IsDebugMode() {
			log.Printf("[DEBUG] [RefreshToken] ✗ API错误: %d - %s", resp.StatusCode, string(body))
		}
		
		// 检查是否是账号锁定错误
		if isAccountLockoutError(resp.StatusCode, string(body)) {
			return nil, &AccountLockoutError{
				StatusCode: resp.StatusCode,
				Body:       string(body),
			}
		}
		
		// 检查是否是refresh token无效错误
		if isRefreshTokenInvalidError(resp.StatusCode, string(body)) {
			return nil, fmt.Errorf("refresh token expired or invalid: status %d, body: %s", resp.StatusCode, string(body))
		}
		
		return nil, fmt.Errorf("failed to refresh token: status %d, body: %s", resp.StatusCode, string(body))
	}
	
	var tokenResp RefreshTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	// 如果响应中没有UserID，尝试从access_token中解析
	if tokenResp.UserID == "" && tokenResp.AccessToken != "" {
		if payload, err := ParseJWT(tokenResp.AccessToken); err == nil {
			// 优先使用 Email，没有则使用 Subject
			if payload.Email != "" {
				tokenResp.UserID = payload.Email
				tokenResp.Email = payload.Email
			} else if payload.Subject != "" {
				tokenResp.UserID = payload.Subject
			}
			
			if IsDebugMode() {
				log.Printf("[DEBUG] [RefreshToken] 从JWT解析UserID: %s", tokenResp.UserID)
				log.Printf("[DEBUG] [RefreshToken] JWT Payload - Email: %s, Subject: %s",
					payload.Email, payload.Subject)
			}
		} else {
			if IsDebugMode() {
				log.Printf("[DEBUG] [RefreshToken] 解析JWT失败: %v", err)
			}
		}
	}
	
	if IsDebugMode() {
		accessTokenPreview := tokenResp.AccessToken
		if len(accessTokenPreview) > 20 {
			accessTokenPreview = accessTokenPreview[:20]
		}
		log.Printf("[DEBUG] [RefreshToken] <<< 刷新成功: UserID=%s, AccessToken=%s..., ExpiresIn=%d",
			tokenResp.UserID,
			accessTokenPreview,
			tokenResp.ExpiresIn)
	}
	
	return &tokenResp, nil
}

// min 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// UpdateAccountToken 更新账号的 token
func UpdateAccountToken(account *model.Account) error {
	if account.RefreshToken == "" {
		return fmt.Errorf("account %s has no refresh token", account.ClientID)
	}
	
	// 调用刷新接口
	tokenResp, err := RefreshAccessToken(account.RefreshToken, account.Proxy)
	if err != nil {
		// 检查是否是账号锁定错误
		if lockoutErr, ok := err.(*AccountLockoutError); ok {
			// 将账号标记为封禁状态
			if markErr := markAccountAsBanned(account, "用户被锁定: "+lockoutErr.Body); markErr != nil {
				log.Printf("[账号管理] 标记账号封禁状态失败: %v", markErr)
			}
		}
		return fmt.Errorf("failed to refresh token for account %s: %w", account.ClientID, err)
	}
	
	// 计算过期时间
	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	
	// 更新数据库
	updates := map[string]interface{}{
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken, // 更新新的 refresh_token
		"token_expiry":  expiry,
		"updated_at":    time.Now(),
	}
	
	if err := database.DB.Model(&model.Account{}).
		Where("id = ?", account.ID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update account token: %w", err)
	}
	
	// 更新内存中的值
	account.AccessToken = tokenResp.AccessToken
	account.RefreshToken = tokenResp.RefreshToken
	account.TokenExpiry = expiry
	
	debugLogf("✅ Refreshed token for account %s, expires at %s", account.ClientID, expiry.Format(time.RFC3339))
	
	return nil
}

// UpdateTokenRecordToken 更新 TokenRecord 的 token
func UpdateTokenRecordToken(record *model.TokenRecord) error {
	if record.RefreshToken == "" {
		return fmt.Errorf("token record %d has no refresh token", record.ID)
	}
	
	// 调用刷新接口
	tokenResp, err := RefreshAccessToken(record.RefreshToken, "")
	if err != nil {
		// 检查是否是账号锁定错误
		if lockoutErr, ok := err.(*AccountLockoutError); ok {
			// 将token记录标记为封禁状态
			if markErr := markTokenRecordAsBanned(record, "账号被锁定: "+lockoutErr.Body); markErr != nil {
				log.Printf("[Token管理] 标记token记录封禁状态失败: %v", markErr)
			}
			// 根据邮箱禁用相关的token记录
			if record.Email != "" {
				if disableErr := disableTokenRecordsByEmail(record.Email, "关联账号被锁定"); disableErr != nil {
					log.Printf("[Token管理] 禁用相关token记录失败: %v", disableErr)
				}
			}
			return fmt.Errorf("token record %d account locked out: %w", record.ID, err)
		}
		
		// 检查是否是refresh token过期错误
		if strings.Contains(err.Error(), "refresh token expired or invalid") {
			// 将token记录标记为过期状态
			if markErr := markTokenRecordAsExpired(record, "Refresh token过期或无效"); markErr != nil {
				log.Printf("[Token管理] 标记token记录过期状态失败: %v", markErr)
			}
			return fmt.Errorf("token record %d refresh token expired: %w", record.ID, err)
		}
		
		return fmt.Errorf("failed to refresh token for record %d: %w", record.ID, err)
	}
	
	// 计算过期时间
	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	
	// 更新数据库
	updates := map[string]interface{}{
		"token":         tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken, // 更新新的 refresh_token
		"token_expiry":  expiry,
		"status":        "active", // 刷新成功时重新激活
		"updated_at":    time.Now(),
	}
	
	if err := database.DB.Model(&model.TokenRecord{}).
		Where("id = ?", record.ID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update token record: %w", err)
	}
	
	// 更新内存中的值
	record.Token = tokenResp.AccessToken
	record.RefreshToken = tokenResp.RefreshToken
	record.TokenExpiry = expiry
	record.Status = "active"
	
	debugLogf("✅ Refreshed token for record %d, expires at %s", record.ID, expiry.Format(time.RFC3339))
	
	return nil
}

// CheckAndRefreshToken 检查并刷新即将过期的 token
func CheckAndRefreshToken(account *model.Account) error {
	// 如果没有 RefreshToken，跳过
	if account.RefreshToken == "" {
		return nil
	}
	
	// 如果 token 在一小时内过期，则刷新
	if time.Until(account.TokenExpiry) < time.Hour {
		debugLogf("⚠️ Token for account %s expires in %v, refreshing...",
			account.ClientID, time.Until(account.TokenExpiry))
		return UpdateAccountToken(account)
	}
	
	return nil
}

// CheckAndRefreshTokenRecord 检查并刷新即将过期的 TokenRecord
func CheckAndRefreshTokenRecord(record *model.TokenRecord) error {
	// 如果没有 RefreshToken，跳过
	if record.RefreshToken == "" {
		return nil
	}
	
	// 如果 token 在一小时内过期，则刷新
	if time.Until(record.TokenExpiry) < time.Hour {
		debugLogf("⚠️ Token for record %d expires in %v, refreshing...",
			record.ID, time.Until(record.TokenExpiry))
		return UpdateTokenRecordToken(record)
	}
	
	return nil
}

// StartTokenRefreshScheduler 启动定时刷新 token 的调度器
func StartTokenRefreshScheduler() {
	go func() {
		// 立即执行一次
		refreshExpiredTokens()
		
		// 然后每分钟检查一次
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		
		for range ticker.C {
			refreshExpiredTokens()
		}
	}()
	
	log.Printf("🔄 Token refresh scheduler started - checking every minute")
}

// refreshExpiredTokens 刷新即将过期的 tokens
func refreshExpiredTokens() {
	now := time.Now()
	threshold := now.Add(time.Hour) // 1小时内即将过期的token
	
	// 查询所有即将过期的账号（排除banned状态）
	var accounts []model.Account
	if err := database.DB.Where("token_expiry < ?", threshold).
		Where("status != ?", "banned").
		Find(&accounts).Error; err == nil {
		
		for _, account := range accounts {
			// 根据账号类型选择不同的刷新方式
			if account.ClientSecret == "refresh-token-login" {
				// refresh-token-login 账号使用 refresh_token 刷新
				if account.RefreshToken != "" {
					if err := UpdateAccountToken(&account); err != nil {
						log.Printf("[Token刷新] ❌ refresh-token账号 %s 刷新失败: %v", account.ClientID, err)
					}
				}
			} else {
				// 普通账号使用 OAuth client credentials 刷新
				if account.ClientID != "" && account.ClientSecret != "" {
					if err := refreshAccountToken(&account); err != nil {
						log.Printf("[Token刷新] ❌ 账号 %s OAuth刷新失败: %v", account.ClientID, err)
					}
				}
			}
		}
	}
	
	// 刷新 TokenRecord 的 tokens - 只排除banned状态的记录
	var records []model.TokenRecord
	if err := database.DB.Where("refresh_token != '' AND token_expiry < ?", threshold).
		Where("status != ?", "banned").
		Find(&records).Error; err == nil {
		
		for _, record := range records {
			if err := UpdateTokenRecordToken(&record); err != nil {
				log.Printf("[Token刷新] ❌ 生成token #%d 刷新失败: %v", record.ID, err)
			}
		}
	}
}

// debugLogf 简单的调试日志函数
func debugLogf(format string, args ...interface{}) {
	if IsDebugMode() {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// RefreshTokenAndAccounts 刷新token记录并异步刷新相同邮箱的账号
func RefreshTokenAndAccounts(tokenRecordID uint) error {
	// 获取token记录
	var record model.TokenRecord
	if err := database.GetDB().First(&record, tokenRecordID).Error; err != nil {
		return fmt.Errorf("获取token记录失败: %w", err)
	}

	if record.RefreshToken == "" {
		return fmt.Errorf("token记录没有refresh_token")
	}

	// 1. 刷新token记录的token
	log.Printf("[Token刷新] 开始刷新token记录 #%d", tokenRecordID)
	
	// 调用刷新接口
	tokenResp, err := RefreshAccessToken(record.RefreshToken, "")
	if err != nil {
		// 检查是否是账号锁定错误
		if lockoutErr, ok := err.(*AccountLockoutError); ok {
			// 将token记录标记为封禁状态
			if markErr := markTokenRecordAsBanned(&record, "账号被锁定: "+lockoutErr.Body); markErr != nil {
				log.Printf("[Token管理] 标记token记录封禁状态失败: %v", markErr)
			}
			// 根据邮箱禁用相关的token记录
			if record.Email != "" {
				if disableErr := disableTokenRecordsByEmail(record.Email, "关联账号被锁定"); disableErr != nil {
					log.Printf("[Token管理] 禁用相关token记录失败: %v", disableErr)
				}
			}
		}
		
		// 检查是否是refresh token过期错误
		if strings.Contains(err.Error(), "refresh token expired or invalid") {
			// 将token记录标记为过期状态
			if markErr := markTokenRecordAsExpired(&record, "Refresh token过期或无效"); markErr != nil {
				log.Printf("[Token管理] 标记token记录过期状态失败: %v", markErr)
			}
		}
		
		return fmt.Errorf("刷新token失败: %w", err)
	}
	
	// 计算过期时间
	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	
	// 更新数据库
	updates := map[string]interface{}{
		"token":         tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"token_expiry":  expiry,
		"status":        "active", // 刷新成功时重置为活跃状态
		"ban_reason":    "",       // 清除封禁原因
		"updated_at":    time.Now(),
	}
	
	if err := database.GetDB().Model(&model.TokenRecord{}).
		Where("id = ?", tokenRecordID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("更新token记录失败: %w", err)
	}
	
	// 2. 解析新token获取邮箱
	email := ""
	if payload, err := ParseJWT(tokenResp.AccessToken); err == nil {
		email = payload.Email
		log.Printf("[Token刷新] 解析到邮箱: %s", email)
	} else {
		log.Printf("[Token刷新] 无法解析JWT获取邮箱: %v", err)
		return nil // 不影响token记录的刷新
	}
	
	if email == "" {
		log.Printf("[Token刷新] 邮箱为空，跳过账号刷新")
		return nil
	}
	
	// 3. 异步刷新相同邮箱的账号
	go refreshAccountsByEmail(email)
	
	return nil
}

// refreshAccountsByEmail 刷新指定邮箱的所有账号
func refreshAccountsByEmail(email string) {
	log.Printf("[账号刷新] 开始刷新邮箱 %s 的所有账号", email)
	
	// 查询所有相同邮箱的账号
	var accounts []model.Account
	if err := database.GetDB().Where("email = ?", email).Find(&accounts).Error; err != nil {
		log.Printf("[账号刷新] 查询邮箱 %s 的账号失败: %v", email, err)
		return
	}
	
	if len(accounts) == 0 {
		log.Printf("[账号刷新] 没有找到邮箱 %s 的账号", email)
		return
	}
	
	log.Printf("[账号刷新] 找到 %d 个账号需要刷新", len(accounts))
	
	// 逐个刷新账号
	successCount := 0
	failCount := 0
	
	for _, account := range accounts {
		// 如果账号没有client_id和client_secret，跳过
		if account.ClientID == "" || account.ClientSecret == "" {
			log.Printf("[账号刷新] 账号 ID:%d 缺少client_id或client_secret，跳过", account.ID)
			continue
		}
		
		log.Printf("[账号刷新] 正在刷新账号 ID:%d (ClientID: %s)", account.ID, account.ClientID)
		
		// 使用OAuth方式刷新token
		if err := refreshAccountToken(&account); err != nil {
			log.Printf("[账号刷新] 账号 ID:%d 刷新失败: %v", account.ID, err)
			failCount++
		} else {
			log.Printf("[账号刷新] 账号 ID:%d 刷新成功", account.ID)
			successCount++
		}
		
		// 添加短暂延迟，避免请求过快
		time.Sleep(100 * time.Millisecond)
	}
	
	log.Printf("[账号刷新] 邮箱 %s 的账号刷新完成 - 成功: %d, 失败: %d",
		email, successCount, failCount)
}

// RefreshAccountToken 使用client credentials刷新账号token（导出函数）
func RefreshAccountToken(account *model.Account) error {
	return refreshAccountToken(account)
}

// refreshAccountToken 使用client credentials刷新账号token
func refreshAccountToken(account *model.Account) error {
	// 构建OAuth token请求
	url := "https://fe.zencoder.ai/oauth/token"
	
	reqBody := map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     account.ClientID,
		"client_secret": account.ClientSecret,
	}
	
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}
	
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	
	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	
	// 使用代理（如果有）
	client := provider.NewHTTPClient(account.Proxy, 30*time.Second)
	
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}
	
	if resp.StatusCode != http.StatusOK {
		// 检查是否是账号锁定错误
		if isAccountLockoutError(resp.StatusCode, string(body)) {
			// 将账号标记为封禁状态
			if markErr := markAccountAsBanned(account, "OAuth认证失败-用户被锁定: "+string(body)); markErr != nil {
				log.Printf("[账号管理] 标记账号封禁状态失败: %v", markErr)
			}
			return &AccountLockoutError{
				StatusCode: resp.StatusCode,
				Body:       string(body),
				AccountID:  account.ClientID,
			}
		}
		return fmt.Errorf("API返回错误: %d - %s", resp.StatusCode, string(body))
	}
	
	// 解析响应
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	
	// 计算过期时间
	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	
	// 解析token获取更多信息
	planType := account.PlanType // 保留原有计划类型
	dailyUsed := account.DailyUsed // 保留原有使用量
	totalUsed := account.TotalUsed // 保留原有总使用量
	
	if payload, err := ParseJWT(tokenResp.AccessToken); err == nil {
		// 更新计划类型（如果有）
		if payload.CustomClaims.Plan != "" {
			planType = model.PlanType(payload.CustomClaims.Plan)
		}
		// 验证邮箱
		if account.Email != "" && payload.Email != account.Email {
			log.Printf("[账号刷新] 警告: 账号 ID:%d 邮箱不匹配 (期望: %s, 实际: %s)",
				account.ID, account.Email, payload.Email)
		}
	}
	
	// 更新数据库
	updates := map[string]interface{}{
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"token_expiry":  expiry,
		"plan_type":     planType,
		"daily_used":    dailyUsed,  // 保持原有使用量
		"total_used":    totalUsed,  // 保持原有总使用量
		"updated_at":    time.Now(),
	}
	
	return database.GetDB().Model(&model.Account{}).
		Where("id = ?", account.ID).
		Updates(updates).Error
}