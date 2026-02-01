package provider

import (
	"fmt"
	"sync"

	"zencoder2api/internal/model"
)

// Manager Provider管理器，缓存已创建的provider实例
type Manager struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

var defaultManager = &Manager{
	providers: make(map[string]Provider),
}

// GetManager 获取默认管理器
func GetManager() *Manager {
	return defaultManager
}

// GetProvider 根据账号和模型获取或创建Provider
func (m *Manager) GetProvider(accountID uint, zenModel model.ZenModel, cfg Config) (Provider, error) {
	key := m.buildKey(accountID, zenModel.ProviderID)

	m.mu.RLock()
	if p, ok := m.providers[key]; ok {
		m.mu.RUnlock()
		return p, nil
	}
	m.mu.RUnlock()

	return m.createProvider(key, zenModel.ProviderID, cfg)
}

func (m *Manager) buildKey(accountID uint, providerID string) string {
	return fmt.Sprintf("%d:%s", accountID, providerID)
}

func (m *Manager) createProvider(key, providerID string, cfg Config) (Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查
	if p, ok := m.providers[key]; ok {
		return p, nil
	}

	var providerType ProviderType
	switch providerID {
	case "openai":
		providerType = ProviderOpenAI
	case "anthropic":
		providerType = ProviderAnthropic
	case "gemini":
		providerType = ProviderGemini
	case "xai":
		providerType = ProviderGrok
	default:
		providerType = ProviderAnthropic
	}

	p, err := NewProvider(providerType, cfg)
	if err != nil {
		return nil, err
	}

	m.providers[key] = p
	return p, nil
}
