package main

import (
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/m-tsuru/muqueue/connector"
	"github.com/m-tsuru/muqueue/handlers"
	"github.com/m-tsuru/muqueue/internal"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	if err := internal.LoadEnvFile(".env"); err != nil {
		panic("failed to load .env file: " + err.Error())
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "gorm.db"
	}
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			panic("failed to create database directory: " + err.Error())
		}
	}

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}
	models := append(
		[]any{
			&internal.User{},
			&internal.Action{},
			&internal.Prompt{},
			&internal.PromptAnswer{},
		},
		connector.AllModels()...,
	)
	if err := db.AutoMigrate(models...); err != nil {
		panic("failed to migrate database: " + err.Error())
	}

	r := gin.Default()
	r.GET("/", handlers.BearerAuthMiddleware(), handlers.PingHandler)

	// API Handlers
	api := r.Group("/api")
	{
		// Webhook endpoints (authenticated by service-specific webhook verification)
		api.POST("/query/misskey", func(c *gin.Context) {
			connector.PostMisskeyWebhookHandler(db, c)
		})

		// Authenticated LLM / API endpoints
		authAPI := api.Group("", handlers.BearerAuthMiddleware())
		{
			authAPI.GET("/", handlers.PingHandler)
			authAPI.GET("/actions", func(c *gin.Context) {
				handlers.GetActionsHandler(db, c)
			})
			authAPI.GET("/actions/:id", func(c *gin.Context) {
				handlers.GetActionByIdHandler(db, c)
			})
			authAPI.PATCH("/actions/:id", func(c *gin.Context) {
				handlers.PatchActionStatusHandler(db, c)
			})
			authAPI.POST("/actions/:id/answer", func(c *gin.Context) {
				handlers.PostAnswerHandler(db, c)
			})
		}
	}

	r.Run()
}
