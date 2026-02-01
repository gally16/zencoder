package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
)

const (
	CredentialGenerateURL = "https://fe.zencoder.ai/frontegg/identity/resources/users/api-tokens/v1"
)

type CredentialGenerateRequest struct {
	Description      string `json:"description"`
	ExpiresInMinutes int    `json:"expiresInMinutes"`
}

type CredentialGenerateResponse struct {
	ClientID     string `json:"clientId"`
	Description  string `json:"description"`
	CreatedAt    string `json:"createdAt"`
	Secret       string `json:"secret"`
	Expires      string `json:"expires"`
	RefreshToken string `json:"refreshToken,omitempty"` // 添加 RefreshToken 字段
}

// GenerateRandomDescription 生成随机5字符描述
func GenerateRandomDescription() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, 5)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}

// GenerateCredential 使用 token 生成一个新凭证
func GenerateCredential(token string) (*CredentialGenerateResponse, error) {
	reqBody := CredentialGenerateRequest{
		Description:      GenerateRandomDescription(),
		ExpiresInMinutes: 525600, // 1 year
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", CredentialGenerateURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "zh-CN,zh;q=0.9,en;q=0.8,zh-TW;q=0.7,ja;q=0.6")
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("frontegg-source", "admin-portal")
	req.Header.Set("origin", "https://auth.zencoder.ai")
	req.Header.Set("pragma", "no-cache")
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("referer", "https://auth.zencoder.ai/")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="143", "Chromium";v="143", "Not A(Brand";v="24"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-site")
	req.Header.Set("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36")
	req.Header.Set("x-frontegg-framework", "next@15.3.8")
	req.Header.Set("x-frontegg-sdk", "@frontegg/nextjs@9.2.10")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result CredentialGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// BatchGenerateCredentials 批量生成凭证
func BatchGenerateCredentials(token string, count int) ([]*CredentialGenerateResponse, []error) {
	var results []*CredentialGenerateResponse
	var errors []error

	for i := 0; i < count; i++ {
		cred, err := GenerateCredential(token)
		if err != nil {
			errors = append(errors, fmt.Errorf("credential %d: %w", i+1, err))
			continue
		}
		results = append(results, cred)
		
		// 添加短暂延迟避免请求过快
		if i < count-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	return results, errors
}
