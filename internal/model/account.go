package model

import (
	"time"
)

type PlanType string

const (
	PlanFree     PlanType = "Free"
	PlanStarter  PlanType = "Starter"
	PlanCore     PlanType = "Core"
	PlanAdvanced PlanType = "Advanced"
	PlanMax      PlanType = "Max"
)

// 每日积分限制
var PlanLimits = map[PlanType]int{
	PlanFree:     30,
	PlanStarter:  280,
	PlanCore:     750,
	PlanAdvanced: 1900,
	PlanMax:      4200,
}

type Account struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	ClientID      string    `json:"client_id" gorm:"uniqueIndex;not null"`
	ClientSecret  string    `json:"-" gorm:"not null"`  // 隐藏不传出
	Email         string    `json:"email" gorm:"index"`
	Category      string    `json:"category" gorm:"default:'normal';index"` // Deprecated: Use Status instead
	Status        string    `json:"status" gorm:"default:'normal';index"`   // normal, cooling, banned, error, disabled
	PlanType      PlanType  `json:"plan_type" gorm:"default:'Free'"`
	Proxy         string    `json:"proxy"`
	AccessToken   string    `json:"-" gorm:"type:text"`
	RefreshToken  string    `json:"-" gorm:"type:text"` // 用于刷新 AccessToken
	TokenExpiry   time.Time `json:"token_expiry"`       // 传出token过期时间
	CreditRefreshTime time.Time `json:"credit_refresh_time"` // 积分刷新时间（来自Zen-Pricing-Period-End）
	IsActive      bool      `json:"is_active" gorm:"default:true"`
	IsCooling     bool      `json:"is_cooling" gorm:"default:false"`
	CoolingUntil  time.Time `json:"cooling_until"` // 冷却结束时间
	BanReason     string    `json:"ban_reason"`    // 封禁/冷却原因
	RateLimitHits int       `json:"rate_limit_hits" gorm:"default:0"` // 429 错误次数
	DailyUsed     float64   `json:"daily_used" gorm:"default:0"`
	TotalUsed     float64   `json:"total_used" gorm:"default:0"`
	LastResetDate         string    `json:"last_reset_date"`
	SubscriptionStartDate time.Time `json:"subscription_start_date"`
	LastUsed              time.Time `json:"last_used"`
	ErrorCount            int       `json:"error_count" gorm:"default:0"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type AccountRequest struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RefreshToken string   `json:"refresh_token"` // Refresh token for authentication
	Token        string   `json:"token"` // Deprecated: Use RefreshToken instead
	Email        string   `json:"email"`
	PlanType     PlanType `json:"plan_type"`
	Proxy        string   `json:"proxy"`
	// Batch generation fields
	GenerateMode bool `json:"generate_mode"` // true for batch generation mode
	GenerateCount int `json:"generate_count"` // number of credentials to generate
}
