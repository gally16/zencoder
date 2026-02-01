package model

import (
	"log"
)

// DebugLogModelMapping 输出模型映射查找日志
func DebugLogModelMapping(requestModel string, zenModel ZenModel, found bool) {
	if found {
		log.Printf("[DEBUG] [ModelMapping] ✓ 找到模型映射: request=%s → id=%s, model=%s, provider=%s, multiplier=%.1f",
			requestModel, zenModel.ID, zenModel.Model, zenModel.ProviderID, zenModel.Multiplier)
		if zenModel.Parameters != nil {
			if zenModel.Parameters.Thinking != nil {
				log.Printf("[DEBUG] [ModelMapping]   └─ thinking: type=%s, budgetTokens=%d",
					zenModel.Parameters.Thinking.Type, zenModel.Parameters.Thinking.BudgetTokens)
			}
			if zenModel.Parameters.ExtraHeaders != nil {
				for k, v := range zenModel.Parameters.ExtraHeaders {
					log.Printf("[DEBUG] [ModelMapping]   └─ extraHeader: %s=%s", k, v)
				}
			}
			if zenModel.Parameters.ForceStreaming != nil && *zenModel.Parameters.ForceStreaming {
				log.Printf("[DEBUG] [ModelMapping]   └─ forceStreaming: true")
			}
		}
	} else {
		log.Printf("[DEBUG] [ModelMapping] ✗ 未找到模型映射: request=%s, 使用默认配置", requestModel)
	}
}
