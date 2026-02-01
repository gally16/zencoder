package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"zencoder2api/internal/database"
	"zencoder2api/internal/model"
)

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

const (
	ZencoderTokenURL = "https://fe.zencoder.ai/oauth/token"
)

func GetToken(account *model.Account) (string, error) {
	if account.AccessToken != "" && time.Now().Before(account.TokenExpiry) {
		return account.AccessToken, nil
	}
	return RefreshToken(account)
}

func RefreshToken(account *model.Account) (string, error) {
	// 每次创建新的 HTTP 客户端，禁用连接复用
	transport := &http.Transport{
		DisableKeepAlives:   true,  // 禁用 Keep-Alive
		DisableCompression:  false,
		MaxIdleConns:        0,     // 不保持空闲连接
		MaxIdleConnsPerHost: 0,
		IdleConnTimeout:     0,
	}

	if account.Proxy != "" {
		proxyURL, err := url.Parse(account.Proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", account.ClientID)
	data.Set("client_secret", account.ClientSecret)

	req, err := http.NewRequest("POST", ZencoderTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Connection", "close") // 明确要求关闭连接

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		// 检查是否是账号锁定错误
		if isAccountLockoutError(resp.StatusCode, string(body)) {
			// 将账号标记为封禁状态
			if markErr := markAccountAsBanned(account, "OAuth认证失败-用户被锁定: "+string(body)); markErr != nil {
				log.Printf("[账号管理] 标记账号封禁状态失败: %v", markErr)
			}
			return "", &AccountLockoutError{
				StatusCode: resp.StatusCode,
				Body:       string(body),
				AccountID:  account.ClientID,
			}
		}
		return "", fmt.Errorf("token request failed: %s", string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}

	account.AccessToken = tokenResp.AccessToken
	account.TokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)

	// 只有已存在的账号才保存到数据库
	if account.ID > 0 {
		database.GetDB().Save(account)
	}

	// 显式关闭传输层，确保连接被清理
	transport.CloseIdleConnections()

	return account.AccessToken, nil
}

func createHTTPClient(proxy string) *http.Client {
	transport := &http.Transport{}

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}
