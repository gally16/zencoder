package model

import (
	"time"
)

// TokenRecord 记录生成账号时使用的token
type TokenRecord struct {
	ID                    uint      `json:"id" gorm:"primaryKey"`
	Token                 string    `json:"token" gorm:"type:text"` // 当前的access token（通过refresh_token生成）
	RefreshToken          string    `json:"refresh_token" gorm:"type:text"` // 用于刷新token的refresh_token，可以为空
	TokenExpiry           time.Time `json:"token_expiry"`            // access token过期时间
	Description           string    `json:"description"`                      // token描述
	Email                 string    `json:"email"`                            // 账号邮箱（从JWT解析）
	PlanType              string    `json:"plan_type"`                        // 订阅等级（从JWT解析）
	SubscriptionStartDate time.Time `json:"subscription_start_date"`          // 订阅开始时间（从JWT解析）
	GeneratedCount        int       `json:"generated_count" gorm:"default:0"` // 已生成账号总数
	LastGeneratedAt       time.Time `json:"last_generated_at"`               // 最后生成时间
	AutoGenerate          bool      `json:"auto_generate" gorm:"default:true"` // 是否自动生成
	Threshold             int       `json:"threshold" gorm:"default:10"`      // 触发自动生成的阈值
	GenerateBatch         int       `json:"generate_batch" gorm:"default:30"` // 每批生成数量
	IsActive              bool      `json:"is_active" gorm:"default:true"`    // 是否激活
	Status                string    `json:"status" gorm:"default:'active'"`   // token状态: active, banned, expired, disabled
	BanReason             string    `json:"ban_reason"`                        // 封禁原因
	HasRefreshToken       bool      `json:"has_refresh_token" gorm:"default:false"` // 是否有refresh_token
	TotalSuccess          int       `json:"total_success" gorm:"default:0"`    // 总成功数
	TotalFail             int       `json:"total_fail" gorm:"default:0"`       // 总失败数
	TotalTasks            int       `json:"total_tasks" gorm:"default:0"`      // 总任务数
	RunningTasks          int       `json:"running_tasks" gorm:"-"`            // 运行中的任务数（不存储在数据库）
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// GenerationTask 生成任务记录
type GenerationTask struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	TokenRecordID uint      `json:"token_record_id" gorm:"index;not null"`
	Token         string    `json:"-" gorm:"type:text"`                   // 实际使用的token
	BatchSize     int       `json:"batch_size"`                           // 批次大小
	SuccessCount  int       `json:"success_count" gorm:"default:0"`      // 成功数量
	FailCount     int       `json:"fail_count" gorm:"default:0"`         // 失败数量
	Status        string    `json:"status" gorm:"default:'pending'"`     // pending, running, completed, failed
	StartedAt     time.Time `json:"started_at"`
	CompletedAt   time.Time `json:"completed_at"`
	ErrorMessage  string    `json:"error_message"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}