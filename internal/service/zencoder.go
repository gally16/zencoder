package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"zencoder2api/internal/model"
)

const (
	ZencoderChatURL = "https://api.zencoder.ai/v1/chat/completions"
	MaxRetries      = 3
	ZencoderVersion = "3.24.0"
)

type ZencoderService struct{}

func NewZencoderService() *ZencoderService {
	return &ZencoderService{}
}

func setZencoderHeaders(req *http.Request, token, modelID string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "zen-cli/0.9.0-windows-x64")
	req.Header.Set("zen-model-id", modelID)
	req.Header.Set("zencoder-arch", "x64")
	req.Header.Set("zencoder-os", "windows")
	req.Header.Set("zencoder-version", ZencoderVersion)
	req.Header.Set("zencoder-client-type", "vscode")
	req.Header.Set("zencoder-operation-id", uuid.New().String())
	req.Header.Set("zencoder-operation-type", "agent_call")
}

func (s *ZencoderService) Chat(req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	// 检查模型是否存在于模型字典中
	zenModel, exists := model.GetZenModel(req.Model)
	if !exists {
		return nil, ErrNoAvailableAccount
	}

	var lastErr error
	for i := 0; i < MaxRetries; i++ {
		account, err := GetNextAccountForModel(req.Model)
		if err != nil {
			return nil, err
		}

		resp, err := s.doRequest(account, req)
		if err != nil {
			MarkAccountError(account)
			lastErr = err
			continue
		}

		ResetAccountError(account)
		
		// ZenCoder服务没有HTTP响应，只能使用模型倍率
		UseCredit(account, zenModel.Multiplier)
		
		return resp, nil
	}

	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

func (s *ZencoderService) doRequest(account *model.Account, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	token, err := GetToken(account)
	if err != nil {
		return nil, err
	}

	// 获取模型映射
	zenModel, exists := model.GetZenModel(req.Model)
	if !exists {
		return nil, ErrNoAvailableAccount
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	client := createHTTPClient(account.Proxy)
	httpReq, err := http.NewRequest("POST", ZencoderChatURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	setZencoderHeaders(httpReq, token, zenModel.ID)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp model.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, err
	}

	return &chatResp, nil
}

func (s *ZencoderService) ChatStream(req *model.ChatCompletionRequest, writer http.ResponseWriter) error {
	// 检查模型是否存在于模型字典中
	zenModel, exists := model.GetZenModel(req.Model)
	if !exists {
		return ErrNoAvailableAccount
	}

	var lastErr error
	for i := 0; i < MaxRetries; i++ {
		account, err := GetNextAccountForModel(req.Model)
		if err != nil {
			return err
		}

		err = s.doStreamRequest(account, req, writer)
		if err != nil {
			MarkAccountError(account)
			lastErr = err
			continue
		}

		ResetAccountError(account)
		
		// 流式响应，使用模型倍率
		UseCredit(account, zenModel.Multiplier)
		
		return nil
	}

	return fmt.Errorf("all retries failed: %w", lastErr)
}

func (s *ZencoderService) doStreamRequest(account *model.Account, req *model.ChatCompletionRequest, writer http.ResponseWriter) error {
	token, err := GetToken(account)
	if err != nil {
		return err
	}

	// 获取模型映射
	zenModel, exists := model.GetZenModel(req.Model)
	if !exists {
		return ErrNoAvailableAccount
	}

	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	client := createHTTPClient(account.Proxy)
	client.Timeout = 5 * time.Minute

	httpReq, err := http.NewRequest("POST", ZencoderChatURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	setZencoderHeaders(httpReq, token, zenModel.ID)

	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	return s.streamResponse(resp.Body, writer)
}

func (s *ZencoderService) streamResponse(body io.Reader, writer http.ResponseWriter) error {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fmt.Fprintf(writer, "%s\n\n", line)
		flusher.Flush()
	}

	return scanner.Err()
}
