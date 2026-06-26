package main

import (
	"fmt"
	"log"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"ger_fin_api/internal/config"
	"ger_fin_api/internal/database"
	"ger_fin_api/internal/migrations"
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

	if err := r.Run(fmt.Sprintf(":%d", cfg.Port)); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
