package main

import (
	"fmt"
	"log"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"ger_fin_api/internal/account"
	"ger_fin_api/internal/auth"
	"ger_fin_api/internal/config"
	"ger_fin_api/internal/database"
	"ger_fin_api/internal/middleware"
	"ger_fin_api/internal/migrations"
	"ger_fin_api/internal/user"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := database.Connect(cfg.Database.DSN())
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	if err := migrations.Run(db); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	// Services
	userSvc    := user.NewService(db)
	authSvc    := auth.NewService(userSvc, cfg.JWT.Secret)
	accountSvc := account.NewService(db)

	// Handlers
	authHandler    := auth.NewHandler(authSvc)
	userHandler    := user.NewHandler(userSvc)
	accountHandler := account.NewHandler(accountSvc)

	// Auth middleware
	jwtAuth := middleware.JWTAuth(cfg.JWT.Secret)

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	api := r.Group("")
	authHandler.RegisterRoutes(api)
	userHandler.RegisterRoutes(api, jwtAuth)
	accountHandler.RegisterRoutes(api, jwtAuth)

	if err := r.Run(fmt.Sprintf(":%d", cfg.Port)); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
