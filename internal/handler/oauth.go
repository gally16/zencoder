package handler

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// PKCESession 存储PKCE会话信息
type PKCESession struct {
	CodeVerifier string
	CreatedAt    time.Time
}

// PKCESessionStore 内存中存储PKCE会话
type PKCESessionStore struct {
	sync.RWMutex
	sessions map[string]*PKCESession
}

// 全局PKCE会话存储
var pkceStore = &PKCESessionStore{
	sessions: make(map[string]*PKCESession),
}

// OAuthHandler OAuth相关处理器
type OAuthHandler struct {
}

// NewOAuthHandler 创建OAuth处理器
func NewOAuthHandler() *OAuthHandler {
	// 启动清理过期会话的定时器
	go cleanupExpiredSessions()
	
	return &OAuthHandler{}
}

// StartOAuthForRT 开始OAuth流程获取RT
func (h *OAuthHandler) StartOAuthForRT(c *gin.Context) {
	// 生成PKCE参数
	codeVerifier, err := generateCodeVerifier(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "生成PKCE参数失败",
		})
		return
	}
	
	// 生成code_challenge
	codeChallenge := generateCodeChallenge(codeVerifier)
	
	// 生成会话ID
	sessionID, err := generateSessionID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "生成会话ID失败",
		})
		return
	}
	
	// 存储会话
	pkceStore.Lock()
	pkceStore.sessions[sessionID] = &PKCESession{
		CodeVerifier: codeVerifier,
		CreatedAt:    time.Now(),
	}
	pkceStore.Unlock()
	
	// 获取回调URL
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	host := c.Request.Host
	
	callbackURL := fmt.Sprintf("%s://%s/api/oauth/callback-rt?session=%s", 
		scheme, host, sessionID)
	
	// 构建state参数
	state := map[string]string{
		"redirectUri":   callbackURL,
		"codeChallenge": codeChallenge,
		"sessionId":     sessionID,
	}
	stateJSON, _ := json.Marshal(state)
	
	// 构建授权URL
	params := url.Values{
		"state":                       {string(stateJSON)},
		"response_type":               {"code"},
		"client_id":                   {"5948a5c5-4b30-4465-a3f2-2136ea53ea0a"},
		"scope":                       {"openid profile email"},
		"redirect_uri":                {"https://auth.zencoder.ai/extension/auth-success"},
		"code_challenge":              {codeChallenge},
		"code_challenge_method":       {"S256"},
	}
	
	authURL := fmt.Sprintf("https://fe.zencoder.ai/oauth/authorize?%s", params.Encode())
	
	// 重定向到授权页面
	c.Redirect(http.StatusFound, authURL)
}

// CallbackOAuthForRT 处理OAuth回调
func (h *OAuthHandler) CallbackOAuthForRT(c *gin.Context) {
	code := c.Query("code")
	sessionID := c.Query("session")
	
	// 验证参数
	if code == "" || sessionID == "" {
		h.renderCallbackPage(c, false, "", "", "缺少必要参数")
		return
	}
	
	// 获取会话
	pkceStore.RLock()
	session, exists := pkceStore.sessions[sessionID]
	pkceStore.RUnlock()
	
	if !exists {
		h.renderCallbackPage(c, false, "", "", "会话已过期，请重新获取")
		return
	}
	
	// 交换token
	tokenResp, err := h.exchangeCodeForToken(code, session.CodeVerifier)
	if err != nil {
		h.renderCallbackPage(c, false, "", "", fmt.Sprintf("获取Token失败: %v", err))
		return
	}
	
	// 清理会话
	pkceStore.Lock()
	delete(pkceStore.sessions, sessionID)
	pkceStore.Unlock()
	
	// 渲染成功页面，传递access token和refresh token
	h.renderCallbackPage(c, true, tokenResp.AccessToken, tokenResp.RefreshToken, "")
}

// exchangeCodeForToken 用授权码换取token
func (h *OAuthHandler) exchangeCodeForToken(code, codeVerifier string) (*OAuthTokenResponse, error) {
	tokenURL := "https://auth.zencoder.ai/api/frontegg/oauth/token"
	
	payload := map[string]string{
		"code":          code,
		"redirect_uri":  "https://auth.zencoder.ai/extension/auth-success",
		"code_verifier": codeVerifier,
		"grant_type":    "authorization_code",
	}
	
	body, _ := json.Marshal(payload)
	
	req, err := http.NewRequest("POST", tokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	
	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-frontegg-sdk", "@frontegg/nextjs@9.2.10")
	req.Header.Set("x-frontegg-framework", "next@15.3.8")
	req.Header.Set("Origin", "https://auth.zencoder.ai")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}
	
	var tokenResp OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}
	
	return &tokenResp, nil
}

// renderCallbackPage 渲染回调页面
func (h *OAuthHandler) renderCallbackPage(c *gin.Context, success bool, accessToken, refreshToken, errorMsg string) {
	html := `
<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>OAuth认证</title>
    <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-50 dark:bg-gray-900 min-h-screen flex items-center justify-center">
    <div class="max-w-md w-full mx-4">
        <div class="bg-white dark:bg-gray-800 rounded-lg shadow-lg p-8">
`
	
	if success {
		html += fmt.Sprintf(`
	           <div class="text-center">
	               <div class="mx-auto flex items-center justify-center h-12 w-12 rounded-full bg-green-100">
	                   <svg class="h-6 w-6 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
	                       <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
	                   </svg>
	               </div>
	               <h2 class="mt-4 text-xl font-semibold text-gray-900 dark:text-white">认证成功！</h2>
	               <p class="mt-2 text-sm text-gray-600 dark:text-gray-400">正在返回并填充Token...</p>
	           </div>
	           <script>
	               // 发送消息给父窗口
	               if (window.opener) {
	                   window.opener.postMessage({
	                       type: 'oauth-rt-complete',
	                       success: true,
	                       accessToken: '%s',
	                       refreshToken: '%s'
	                   }, window.location.origin);
	                   
	                   // 2秒后关闭窗口
	                   setTimeout(() => {
	                       window.close();
	                   }, 2000);
	               }
	           </script>
`, accessToken, refreshToken)
	} else {
		html += fmt.Sprintf(`
            <div class="text-center">
                <div class="mx-auto flex items-center justify-center h-12 w-12 rounded-full bg-red-100">
                    <svg class="h-6 w-6 text-red-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
                    </svg>
                </div>
                <h2 class="mt-4 text-xl font-semibold text-gray-900 dark:text-white">认证失败</h2>
                <p class="mt-2 text-sm text-gray-600 dark:text-gray-400">%s</p>
                <button onclick="window.close()" class="mt-4 px-4 py-2 bg-gray-600 text-white rounded-lg hover:bg-gray-700 transition-colors">
                    关闭窗口
                </button>
            </div>
            <script>
                // 发送错误消息给父窗口
                if (window.opener) {
                    window.opener.postMessage({
                        type: 'oauth-rt-complete',
                        success: false,
                        error: '%s'
                    }, window.location.origin);
                }
            </script>
`, errorMsg, errorMsg)
	}
	
	html += `
        </div>
    </div>
</body>
</html>
`
	
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, html)
}

// OAuthTokenResponse OAuth token响应
type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// generateCodeVerifier 生成PKCE code_verifier
func generateCodeVerifier(length int) (string, error) {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	result := make([]byte, length)
	randomBytes := make([]byte, length)
	
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	
	for i := 0; i < length; i++ {
		result[i] = chars[int(randomBytes[i])%len(chars)]
	}
	
	return string(result), nil
}

// generateCodeChallenge 生成PKCE code_challenge
func generateCodeChallenge(codeVerifier string) string {
	hash := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// generateSessionID 生成会话ID
func generateSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// cleanupExpiredSessions 清理过期的PKCE会话
func cleanupExpiredSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		pkceStore.Lock()
		now := time.Now()
		for id, session := range pkceStore.sessions {
			if now.Sub(session.CreatedAt) > 10*time.Minute {
				delete(pkceStore.sessions, id)
			}
		}
		pkceStore.Unlock()
	}
}