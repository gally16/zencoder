package model

import (
	"sort"
	"sync"
	"time"
)

// ThinkingConfig thinking模式配置
type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budgetTokens"`
	Signature    string `json:"signature,omitempty"`
}

// ReasoningConfig OpenAI reasoning配置
type ReasoningConfig struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary,omitempty"`
}

// TextConfig OpenAI text配置
type TextConfig struct {
	Verbosity string `json:"verbosity"`
}

// ModelParameters 模型参数配置
type ModelParameters struct {
	Temperature    *float64          `json:"temperature,omitempty"`
	Thinking       *ThinkingConfig   `json:"thinking,omitempty"`
	Reasoning      *ReasoningConfig  `json:"reasoning,omitempty"`
	Text           *TextConfig       `json:"text,omitempty"`
	ExtraHeaders   map[string]string `json:"extraHeaders,omitempty"`
	ForceStreaming *bool             `json:"forceStreaming,omitempty"`
}

type ZenModel struct {
	ID          string           `json:"id"`
	DisplayName string           `json:"displayName"`
	Model       string           `json:"model"`
	Multiplier  float64          `json:"multiplier"`
	ProviderID  string           `json:"providerId"`
	Parameters  *ModelParameters `json:"parameters,omitempty"`
	IsHidden    bool             `json:"isHidden"`
	PremiumOnly bool             `json:"premiumOnly"` // 仅Advanced/Max可用
}

// 辅助变量
var (
	temp0       = 0.0
	temp1       = 1.0
	forceStream = true

	// Thinking模式参数
	thinkingParams = &ModelParameters{
		Temperature: &temp1,
		Thinking:    &ThinkingConfig{Type: "enabled", BudgetTokens: 4096},
		ExtraHeaders: map[string]string{
			"anthropic-beta": "interleaved-thinking-2025-05-14",
		},
	}

	// OpenAI reasoning参数
	openaiParams = &ModelParameters{
		Temperature: &temp1,
		Reasoning:   &ReasoningConfig{Effort: "medium", Summary: "auto"},
		Text:        &TextConfig{Verbosity: "medium"},
	}
)

// 默认模型映射表，用于启动引导和上游同步失败时的回退。
var defaultZenModels = map[string]ZenModel{
	// Anthropic Models - Thinking模式（通过ID访问）
	"claude-haiku-4-5-20251001-thinking": {
		ID: "haiku-4-5-think", DisplayName: "Haiku 4.5 Parallel Thinking",
		Model: "claude-haiku-4-5-20251001", Multiplier: 1, ProviderID: "anthropic",
		Parameters: thinkingParams,
	},
	"claude-sonnet-4-20250514-thinking": {
		ID: "sonnet-4-think", DisplayName: "Sonnet 4 Parallel Thinking",
		Model: "claude-sonnet-4-20250514", Multiplier: 3, ProviderID: "anthropic",
		Parameters: thinkingParams,
		IsHidden:   true,
	},
	"claude-sonnet-4-5-20250929-thinking": {
		ID: "sonnet-4-5-think", DisplayName: "Sonnet 4.5 Parallel Thinking",
		Model: "claude-sonnet-4-5-20250929", Multiplier: 3, ProviderID: "anthropic",
		Parameters: thinkingParams,
	},
	"claude-opus-4-1-20250805-thinking": {
		ID: "opus-4-think", DisplayName: "Opus 4.1 Parallel Thinking",
		Model: "claude-opus-4-1-20250805", Multiplier: 15, ProviderID: "anthropic",
		PremiumOnly: true,
		Parameters: &ModelParameters{
			Temperature:    &temp1,
			Thinking:       &ThinkingConfig{Type: "enabled", BudgetTokens: 4096},
			ExtraHeaders:   map[string]string{"anthropic-beta": "interleaved-thinking-2025-05-14"},
			ForceStreaming: &forceStream,
		},
		IsHidden: true,
	},
	"claude-opus-4-5-20251101-thinking": {
		ID: "opus-4-5-think", DisplayName: "Opus 4.5 Parallel Thinking",
		Model: "claude-opus-4-5-20251101", Multiplier: 5, ProviderID: "anthropic",
		PremiumOnly: true,
		Parameters: &ModelParameters{
			Temperature:    &temp1,
			Thinking:       &ThinkingConfig{Type: "enabled", BudgetTokens: 4096},
			ExtraHeaders:   map[string]string{"anthropic-beta": "interleaved-thinking-2025-05-14"},
			ForceStreaming: &forceStream,
		},
	},
	// Anthropic Models - 标准模式（不带 Thinking）
	"claude-sonnet-4-20250514": {
		ID: "sonnet-4", DisplayName: "Sonnet 4",
		Model: "claude-sonnet-4-20250514", Multiplier: 2, ProviderID: "anthropic",
	},
	"claude-sonnet-4-5-20250929": {
		ID: "sonnet-4-5", DisplayName: "Sonnet 4.5",
		Model: "claude-sonnet-4-5-20250929", Multiplier: 2, ProviderID: "anthropic",
	},
	"claude-opus-4-1-20250805": {
		ID: "opus-4", DisplayName: "Opus 4.1",
		Model: "claude-opus-4-1-20250805", Multiplier: 10, ProviderID: "anthropic",
		PremiumOnly: true,
		Parameters:  &ModelParameters{ForceStreaming: &forceStream},
	},
	"claude-opus-4-5-20251101": { //非原生实现
		ID: "opus-4-5-think", DisplayName: "Opus 4.5 Parallel Thinking",
		Model: "claude-opus-4-5-20251101", Multiplier: 5, ProviderID: "anthropic",
		PremiumOnly: true,
		Parameters: &ModelParameters{
			Temperature:    &temp1,
			Thinking:       &ThinkingConfig{Type: "enabled", BudgetTokens: 4096},
			ExtraHeaders:   map[string]string{"anthropic-beta": "interleaved-thinking-2025-05-14"},
			ForceStreaming: &forceStream,
		},
	},
	"claude-haiku-4-5-20251001": { //非原生实现
		ID: "haiku-4-5-think", DisplayName: "Haiku 4.5 Parallel Thinking",
		Model: "claude-haiku-4-5-20251001", Multiplier: 1, ProviderID: "anthropic",
		Parameters: thinkingParams,
	},
	// Gemini Models
	"gemini-3-pro-preview": {
		ID: "gemini-3-pro-preview", DisplayName: "Gemini Pro 3.0",
		Model: "gemini-3-pro-preview", Multiplier: 2, ProviderID: "gemini",
		Parameters: &ModelParameters{Temperature: &temp1},
	},
	"gemini-3-flash-preview": {
		ID: "gemini-3-flash-preview", DisplayName: "Gemini Flash 3.0",
		Model: "gemini-3-flash-preview", Multiplier: 1, ProviderID: "gemini",
		Parameters: &ModelParameters{Temperature: &temp1},
		IsHidden:   true,
	},

	// OpenAI Models
	"gpt-5.1-codex-mini": {
		ID: "gpt-5-1-codex-mini", DisplayName: "GPT-5.1 Codex mini",
		Model: "gpt-5.1-codex-mini", Multiplier: 0.5, ProviderID: "openai",
		Parameters: openaiParams,
	},
	"gpt-5.1-codex": {
		ID: "gpt-5-1-codex-medium", DisplayName: "GPT-5.1 Codex",
		Model: "gpt-5.1-codex", Multiplier: 1, ProviderID: "openai",
		Parameters: openaiParams,
		IsHidden:   true,
	},
	"gpt-5.1-codex-max": {
		ID: "gpt-5-1-codex-max", DisplayName: "GPT-5.1 Codex Max",
		Model: "gpt-5.1-codex-max", Multiplier: 1.5, ProviderID: "openai",
		Parameters: openaiParams,
	},
	"gpt-5.2-codex": {
		ID: "gpt-5-2-codex", DisplayName: "GPT-5.2 Codex",
		Model: "gpt-5.2-codex", Multiplier: 2, ProviderID: "openai",
		Parameters: openaiParams,
	},
	"gpt-5-2025-08-07": {
		ID: "gpt-5-medium", DisplayName: "GPT-5",
		Model: "gpt-5-2025-08-07", Multiplier: 1, ProviderID: "openai",
		Parameters: openaiParams,
		IsHidden:   true,
	},
	"gpt-5-codex": {
		ID: "gpt-5-codex-medium", DisplayName: "GPT-5-Codex",
		Model: "gpt-5-codex", Multiplier: 1, ProviderID: "openai",
		Parameters: openaiParams,
		IsHidden:   true,
	},

	// xAI Models
	"grok-code-fast-1": {
		ID: "grok-code-fast", DisplayName: "Grok Code Fast 1",
		Model: "grok-code-fast-1", Multiplier: 0.25, ProviderID: "xai",
		Parameters: &ModelParameters{Temperature: &temp0},
	},

	// Utility Models
	"gpt-5-nano-2025-08-07": {
		ID: "generate-name-v2", DisplayName: "Cheap model for generating names",
		Model: "gpt-5-nano-2025-08-07", Multiplier: 0, ProviderID: "openai",
		Parameters: &ModelParameters{
			Reasoning: &ReasoningConfig{Effort: "minimal"},
		},
	},
}

var (
	zenModelsMu       sync.RWMutex
	zenModels         = cloneZenModels(defaultZenModels)
	zenModelsSyncedAt time.Time
)

func cloneZenModels(src map[string]ZenModel) map[string]ZenModel {
	dst := make(map[string]ZenModel, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// DefaultZenModels 返回默认模型集合副本。
func DefaultZenModels() map[string]ZenModel {
	return cloneZenModels(defaultZenModels)
}

// ReplaceZenModels 原子替换当前模型集合。
func ReplaceZenModels(models map[string]ZenModel) {
	zenModelsMu.Lock()
	defer zenModelsMu.Unlock()

	zenModels = cloneZenModels(models)
	zenModelsSyncedAt = time.Now()
}

// ResetZenModelsToDefault 在同步失败时回退到默认模型集合。
func ResetZenModelsToDefault() {
	ReplaceZenModels(defaultZenModels)
}

// ZenModelsSyncedAt 返回最近一次模型表更新时间。
func ZenModelsSyncedAt() time.Time {
	zenModelsMu.RLock()
	defer zenModelsMu.RUnlock()
	return zenModelsSyncedAt
}

// GetZenModel 获取模型配置，如果不存在则返回空模型和false
func GetZenModel(modelID string) (ZenModel, bool) {
	zenModelsMu.RLock()
	defer zenModelsMu.RUnlock()

	if m, ok := zenModels[modelID]; ok {
		return m, true
	}
	// 模型不存在，返回空模型和false
	return ZenModel{}, false
}

// ListZenModels 返回稳定排序后的模型列表。
func ListZenModels() []ZenModel {
	zenModelsMu.RLock()
	defer zenModelsMu.RUnlock()

	models := make([]ZenModel, 0, len(zenModels))
	for _, m := range zenModels {
		models = append(models, m)
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].Model < models[j].Model
	})

	return models
}

// CanUseModel 检查订阅类型是否可以使用指定模型
func CanUseModel(planType PlanType, modelID string) bool {
	zenModel, _ := GetZenModel(modelID)

	// Advanced和Max可以使用所有模型
	if planType == PlanAdvanced || planType == PlanMax {
		return true
	}

	// 其他订阅类型不能使用PremiumOnly模型
	return !zenModel.PremiumOnly
}
