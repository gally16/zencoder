package service

import (
	"fmt"
	"net/http"

	"zencoder2api/internal/model"
	"zencoder2api/internal/service/provider"
)

type APIService struct {
	manager *provider.Manager
}

func NewAPIService() *APIService {
	return &APIService{
		manager: provider.GetManager(),
	}
}

func (s *APIService) Chat(req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	// 检查模型是否存在于模型字典中
	_, exists := model.GetZenModel(req.Model)
	if !exists {
		return nil, ErrNoAvailableAccount
	}

	var lastErr error

	for i := 0; i < MaxRetries; i++ {
		account, err := GetNextAccountForModel(req.Model)
		if err != nil {
			return nil, err
		}

		resp, err := s.doChat(account, req)
		if err != nil {
			MarkAccountError(account)
			lastErr = err
			continue
		}

		ResetAccountError(account)
		zenModel, exists := model.GetZenModel(req.Model)
		if !exists {
			// 模型不存在，使用默认倍率
			UseCredit(account, 1.0)
		} else {
			// API服务没有HTTP响应，只能使用模型倍率
			UseCredit(account, zenModel.Multiplier)
		}
		
		return resp, nil
	}

	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

func (s *APIService) doChat(account *model.Account, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	zenModel, exists := model.GetZenModel(req.Model)
	if !exists {
		return nil, ErrNoAvailableAccount
	}

	cfg := s.buildConfig(account, zenModel)
	p, err := s.manager.GetProvider(account.ID, zenModel, cfg)
	if err != nil {
		return nil, err
	}

	// 注意：已移除模型重定向逻辑，直接使用用户请求的模型名

	return p.Chat(req)
}

func (s *APIService) buildConfig(account *model.Account, zenModel model.ZenModel) provider.Config {
	cfg := provider.Config{
		APIKey: account.AccessToken,
		Proxy:  account.Proxy,
	}

	// 设置额外请求头
	if zenModel.Parameters != nil && zenModel.Parameters.ExtraHeaders != nil {
		cfg.ExtraHeaders = zenModel.Parameters.ExtraHeaders
	}

	return cfg
}

func (s *APIService) ChatStream(req *model.ChatCompletionRequest, writer http.ResponseWriter) error {
	// 检查模型是否存在于模型字典中
	_, exists := model.GetZenModel(req.Model)
	if !exists {
		return ErrNoAvailableAccount
	}

	var lastErr error

	for i := 0; i < MaxRetries; i++ {
		account, err := GetNextAccountForModel(req.Model)
		if err != nil {
			return err
		}

		err = s.doChatStream(account, req, writer)
		if err != nil {
			MarkAccountError(account)
			lastErr = err
			continue
		}

		ResetAccountError(account)
		zenModel, exists := model.GetZenModel(req.Model)
		if !exists {
			// 模型不存在，使用默认倍率
			UseCredit(account, 1.0)
		} else {
			// 流式响应，使用模型倍率
			UseCredit(account, zenModel.Multiplier)
		}
		
		return nil
	}

	return fmt.Errorf("all retries failed: %w", lastErr)
}

func (s *APIService) doChatStream(account *model.Account, req *model.ChatCompletionRequest, writer http.ResponseWriter) error {
	zenModel, exists := model.GetZenModel(req.Model)
	if !exists {
		return ErrNoAvailableAccount
	}

	cfg := s.buildConfig(account, zenModel)
	p, err := s.manager.GetProvider(account.ID, zenModel, cfg)
	if err != nil {
		return err
	}

	// 注意：已移除模型重定向逻辑，直接使用用户请求的模型名

	return p.ChatStream(req, writer)
}
