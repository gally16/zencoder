package provider

import "fmt"

type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
	ProviderGemini    ProviderType = "gemini"
	ProviderGrok      ProviderType = "grok"
)

func NewProvider(providerType ProviderType, cfg Config) (Provider, error) {
	switch providerType {
	case ProviderOpenAI:
		return NewOpenAIProvider(cfg), nil
	case ProviderAnthropic:
		return NewAnthropicProvider(cfg), nil
	case ProviderGemini:
		return NewGeminiProvider(cfg)
	case ProviderGrok:
		return NewGrokProvider(cfg), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, providerType)
	}
}
