package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	_ "ger_fin_api/docs"
	"ger_fin_api/internal/account"
	"ger_fin_api/internal/auth"
	"ger_fin_api/internal/budget"
	"ger_fin_api/internal/category"
	"ger_fin_api/internal/changes"
	"ger_fin_api/internal/config"
	"ger_fin_api/internal/currency"
	"ger_fin_api/internal/database"
	"ger_fin_api/internal/middleware"
	"ger_fin_api/internal/migrations"
	recurringTx "ger_fin_api/internal/recurring_transaction"
	"ger_fin_api/internal/report"
	"ger_fin_api/internal/transaction"
	"ger_fin_api/internal/user"
)

// @title			GerFin API
// @version			1.0
// @description		Personal finance management API
// @host			localhost:3000
// @BasePath		/api/v1
// @securityDefinitions.apikey BearerAuth
// @in				header
// @name			Authorization
// @description		Type "Bearer" followed by a space and your JWT token.
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
	userSvc     := user.NewService(db)
	authSvc     := auth.NewService(userSvc, cfg.JWT.Secret, cfg.JWT.ExpiresIn)
	accountSvc  := account.NewService(db)
	categorySvc := category.NewService(db)

	if err := categorySvc.SeedDefaultCategories(); err != nil {
		log.Fatalf("failed to seed default categories: %v", err)
	}

	currencySvc := currency.NewService(db, cfg.OXRAppID)
	if err := currencySvc.FetchAndStoreRates(); err != nil {
		log.Printf("initial rate fetch failed (continuing with cached rates): %v", err)
	}

	transactionSvc := transaction.NewService(db)
	recurringTxSvc := recurringTx.NewService(db, transactionSvc)
	reportSvc      := report.NewService(db, currencySvc)
	budgetSvc      := budget.NewService(db, currencySvc, userSvc)
	changesSvc     := changes.NewService(db)


	// Schedulers
	recurringScheduler := recurringTx.NewScheduler(recurringTxSvc)
	recurringScheduler.Start()
	defer recurringScheduler.Stop()

	currencyScheduler := currency.NewScheduler(currencySvc)
	currencyScheduler.Start()
	defer currencyScheduler.Stop()

	budgetScheduler := budget.NewScheduler(budgetSvc)
	budgetScheduler.Start()
	defer budgetScheduler.Stop()

	// Handlers
	tokenStore         := auth.NewTokenStore(db)
	refreshStore       := auth.NewRefreshStore(db, cfg.JWT.RefreshTTL)
	authHandler        := auth.NewHandler(authSvc, tokenStore, refreshStore)
	userHandler        := user.NewHandler(userSvc)
	accountHandler     := account.NewHandler(accountSvc, currencySvc, userSvc)
	categoryHandler    := category.NewHandler(categorySvc)
	transactionHandler := transaction.NewHandler(transactionSvc, currencySvc, userSvc)
	recurringTxHandler := recurringTx.NewHandler(recurringTxSvc)
	reportHandler      := report.NewHandler(reportSvc, userSvc)
	currencyHandler    := currency.NewHandler(currencySvc)
	budgetHandler      := budget.NewHandler(budgetSvc)
	changesHandler     := changes.NewHandler(changesSvc)


	// Auth middleware
	jwtAuth := middleware.JWTAuth(cfg.JWT.Secret, tokenStore)

	r := gin.Default()
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatalf("invalid TRUSTED_PROXIES: %v", err)
	}

	r.Use(cors.New(cors.Config{
		AllowAllOrigins: len(cfg.CORSAllowedOrigins) == 0,
		AllowOrigins:    cfg.CORSAllowedOrigins,
		AllowMethods:    []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:    []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:   []string{"Content-Length"},
	}))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := r.Group("/api/v1", middleware.ValidateIDParams())
	authHandler.RegisterRoutes(api.Group("", middleware.RateLimit(20, time.Minute)), jwtAuth)
	userHandler.RegisterRoutes(api, jwtAuth)
	accountHandler.RegisterRoutes(api, jwtAuth)
	categoryHandler.RegisterRoutes(api, jwtAuth)
	transactionHandler.RegisterRoutes(api, jwtAuth)
	recurringTxHandler.RegisterRoutes(api, jwtAuth)
	reportHandler.RegisterRoutes(api, jwtAuth)
	currencyHandler.RegisterRoutes(api, jwtAuth)
	budgetHandler.RegisterRoutes(api, jwtAuth)
	changesHandler.RegisterRoutes(api, jwtAuth)


	if err := r.Run(fmt.Sprintf(":%d", cfg.Port)); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
