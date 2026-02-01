package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"zencoder2api/internal/database"
	"zencoder2api/internal/handler"
	"zencoder2api/internal/middleware"
	"zencoder2api/internal/service"
)

func main() {
	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading it, using system environment variables or defaults")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "7860" // 默认使用7860端口，兼容Huggingface Spaces
	}

	// 数据库初始化
	dbType := os.Getenv("DB_TYPE")
	dbDSN := os.Getenv("DATABASE_URL")

	// 向后兼容：如果没有设置 DB_TYPE 和 DATABASE_URL，使用 DB_PATH
	if dbType == "" && dbDSN == "" {
		dbType = "sqlite"
		dbDSN = os.Getenv("DB_PATH")
		if dbDSN == "" {
			dbDSN = "data.db"
		}
	}

	if err := database.Init(dbType, dbDSN); err != nil {
		log.Fatal("Failed to init database:", err)
	}

	// 启动积分重置定时任务
	service.StartCreditResetScheduler()

	// 启动Token刷新定时任务
	service.StartTokenRefreshScheduler()

	// 初始化账号池
	service.InitAccountPool()

	// 初始化自动生成服务
	service.InitAutoGenerationService()

	r := gin.Default()
	setupRoutes(r)

	log.Printf("Server starting on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

func setupRoutes(r *gin.Engine) {
	r.Static("/static", "./web/static")
	r.LoadHTMLGlob("web/templates/*")

	r.GET("/", func(c *gin.Context) {
		c.HTML(200, "index.html", nil)
	})

	// Anthropic API - /v1/messages
	anthropicHandler := handler.NewAnthropicHandler()
	r.POST("/v1/messages", middleware.LoggerMiddleware(), middleware.AuthMiddleware(), anthropicHandler.Messages)

	// OpenAI API - /v1/chat/completions, /v1/responses
	openaiHandler := handler.NewOpenAIHandler()
	r.POST("/v1/chat/completions", middleware.LoggerMiddleware(), middleware.AuthMiddleware(), openaiHandler.ChatCompletions)
	r.POST("/v1/responses", middleware.LoggerMiddleware(), middleware.AuthMiddleware(), openaiHandler.Responses)

	// Gemini API - /v1beta/models/*path
	geminiHandler := handler.NewGeminiHandler()
	r.POST("/v1beta/models/*path", middleware.LoggerMiddleware(), middleware.AuthMiddleware(), geminiHandler.HandleRequest)

	// OAuth处理器 - 不需要管理密码验证（公开访问）
	oauthHandler := handler.NewOAuthHandler()
	r.GET("/api/oauth/start-rt", oauthHandler.StartOAuthForRT)
	r.GET("/api/oauth/callback-rt", oauthHandler.CallbackOAuthForRT)
	r.POST("/api/oauth/exchange", oauthHandler.ManualExchange)

	// External API - 用于注册机提交OAuth token（公开访问）
	externalHandler := handler.NewExternalHandler()
	r.POST("/api/external/submit-tokens", externalHandler.SubmitTokens)

	// Account management API - 需要后台管理密码验证
	accountHandler := handler.NewAccountHandler()
	tokenHandler := handler.NewTokenHandler()
	api := r.Group("/api")
	api.Use(middleware.AdminAuthMiddleware()) // 应用后台管理密码验证中间件
	{
		// 账号管理
		api.GET("/accounts", accountHandler.List)
		api.POST("/accounts", accountHandler.Create)
		api.PUT("/accounts/:id", accountHandler.Update)
		api.DELETE("/accounts/:id", accountHandler.Delete)
		api.POST("/accounts/:id/toggle", accountHandler.Toggle)
		api.POST("/accounts/batch/category", accountHandler.BatchUpdateCategory)
		api.POST("/accounts/batch/move-all", accountHandler.BatchMoveAll)
		api.POST("/accounts/batch/refresh-token", accountHandler.BatchRefreshToken)
		api.POST("/accounts/batch/delete", accountHandler.BatchDelete)

		// Token记录管理
		api.GET("/tokens", tokenHandler.ListTokenRecords)
		api.PUT("/tokens/:id", tokenHandler.UpdateTokenRecord)
		api.DELETE("/tokens/:id", tokenHandler.DeleteTokenRecord)
		api.POST("/tokens/:id/trigger", tokenHandler.TriggerGeneration)
		api.POST("/tokens/:id/refresh", tokenHandler.RefreshTokenRecord)
		api.GET("/tokens/tasks", tokenHandler.GetGenerationTasks)
		api.GET("/tokens/pool-status", tokenHandler.GetPoolStatus)
	}
}
