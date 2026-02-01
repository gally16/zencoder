package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
)

var (
	debugMode     bool
	debugModeOnce sync.Once
)

// IsDebugMode 检查是否启用调试模式
func IsDebugMode() bool {
	debugModeOnce.Do(func() {
		debugMode = os.Getenv("DEBUG") == "true" || os.Getenv("DEBUG") == "1"
	})
	return debugMode
}

// RequestLogger 用于收集请求级日志
type RequestLogger struct {
	logs     []string
	mu       sync.Mutex
	hasError bool
}

// NewRequestLogger 创建新的请求日志记录器
func NewRequestLogger() *RequestLogger {
	return &RequestLogger{
		logs: make([]string, 0, 20),
	}
}

// Log 记录一条日志
func (l *RequestLogger) Log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	
	// 如果全局 DEBUG 开启，直接打印
	if IsDebugMode() {
		log.Print("[DEBUG] " + msg)
		return 
	}

	// 否则缓冲
	l.mu.Lock()
	l.logs = append(l.logs, "[DEBUG] " + msg)
	l.mu.Unlock()
}

// MarkError 标记发生错误
func (l *RequestLogger) MarkError() {
	l.mu.Lock()
	l.hasError = true
	l.mu.Unlock()
}

// Flush 输出缓冲的日志（如果有错误）
func (l *RequestLogger) Flush() {
	// 只有在非 Debug 模式且发生错误时才需要 Flush (Debug 模式下已经实时打印了)
	if !IsDebugMode() && l.hasError {
		l.mu.Lock()
		defer l.mu.Unlock()
		for _, msg := range l.logs {
			log.Print(msg)
		}
	}
}

type contextKey string

const loggerContextKey contextKey = "request_logger"

// WithLogger 将 logger 注入 context
func WithLogger(ctx context.Context, logger *RequestLogger) context.Context {
	return context.WithValue(ctx, loggerContextKey, logger)
}

// GetLogger 从 context 获取 logger
func GetLogger(ctx context.Context) *RequestLogger {
	val := ctx.Value(loggerContextKey)
	if val != nil {
		if logger, ok := val.(*RequestLogger); ok {
			return logger
		}
	}
	return nil
}

// 辅助函数：获取 logger 并记录
func logToContext(ctx context.Context, format string, args ...interface{}) {
	logger := GetLogger(ctx)
	if logger != nil {
		logger.Log(format, args...)
	} else if IsDebugMode() {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// DebugLog 调试日志输出
func DebugLog(ctx context.Context, format string, args ...interface{}) {
	logToContext(ctx, format, args...)
}

// DebugLogRequest 请求开始日志
func DebugLogRequest(ctx context.Context, provider, endpoint, model string) {
	logToContext(ctx, "[%s] >>> 请求开始: endpoint=%s, model=%s", provider, endpoint, model)
}

// DebugLogRetry 重试日志
func DebugLogRetry(ctx context.Context, provider string, attempt int, accountID uint, err error) {
    if logger := GetLogger(ctx); logger != nil {
        logger.MarkError()
    }
	logToContext(ctx, "[%s] ↻ 重试 #%d: accountID=%d, error=%v", provider, attempt, accountID, err)
}

// DebugLogAccountSelected 账号选择日志
func DebugLogAccountSelected(ctx context.Context, provider string, accountID uint, email string) {
	logToContext(ctx, "[%s] ✓ 选择账号: id=%d, email=%s", provider, accountID, email)
}

// DebugLogRequestSent 请求发送日志
func DebugLogRequestSent(ctx context.Context, provider, url string) {
	logToContext(ctx, "[%s] → 发送请求: %s", provider, url)
}

// DebugLogResponseReceived 响应接收日志
func DebugLogResponseReceived(ctx context.Context, provider string, statusCode int) {
	logToContext(ctx, "[%s] ← 收到响应: status=%d", provider, statusCode)
}

// DebugLogRequestEnd 请求结束日志
func DebugLogRequestEnd(ctx context.Context, provider string, success bool, err error) {
    if !success || err != nil {
        if logger := GetLogger(ctx); logger != nil {
            logger.MarkError()
        }
		logToContext(ctx, "[%s] <<< 请求完成: success=false, error=%v", provider, err)
    } else {
        logToContext(ctx, "[%s] <<< 请求完成: success=true", provider)
    }
}

// DebugLogRequestHeaders 请求头日志
func DebugLogRequestHeaders(ctx context.Context, provider string, headers map[string][]string) {
	logToContext(ctx, "[%s] 请求头:", provider)
	for k, v := range headers {
		// 隐藏敏感信息
		if k == "Authorization" || k == "x-api-key" {
			logToContext(ctx, "[%s]   %s: ***", provider, k)
		} else {
			logToContext(ctx, "[%s]   %s: %v", provider, k, v)
		}
	}
}

// DebugLogResponseHeaders 响应头日志
func DebugLogResponseHeaders(ctx context.Context, provider string, headers map[string][]string) {
	logToContext(ctx, "[%s] 响应头:", provider)
	for k, v := range headers {
		// 隐藏敏感信息
		if k == "X-Api-Key" || k == "Authorization" {
			logToContext(ctx, "[%s]   %s: ***", provider, k)
		} else {
			logToContext(ctx, "[%s]   %s: %v", provider, k, v)
		}
	}
}

// DebugLogActualModel 实际调用模型日志
func DebugLogActualModel(ctx context.Context, provider, requestModel, actualModel string) {
	logToContext(ctx, "[%s] 模型映射: %s → %s", provider, requestModel, actualModel)
}

// DebugLogErrorResponse 错误响应内容日志
func DebugLogErrorResponse(ctx context.Context, provider string, statusCode int, body string) {
    if logger := GetLogger(ctx); logger != nil {
        logger.MarkError()
    }
	logToContext(ctx, "[%s] ✗ 错误响应 [%d]: %s", provider, statusCode, body)
}
