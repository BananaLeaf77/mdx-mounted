package bootstrap

import (
	"chronosphere/config"
	"chronosphere/delivery"
	"chronosphere/middleware"
	"chronosphere/repository"
	"chronosphere/service"
	"chronosphere/utils"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// initApp contains all shared bootstrap logic.
// waEnabled  — pass false to skip WhatsApp entirely (no-wa mode)
// limiterEnabled — pass false to skip rate limiter (no-limiter mode)
func initApp(waEnabled, limiterEnabled bool) (*gin.Engine, *gorm.DB, *cron.Cron) {
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  .env file not found, using system environment variables")
	}

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		utils.RegisterCustomValidations(v)
	}

	db, dbAddr, err := config.BootDB()
	if err != nil {
		log.Fatal("❌ Failed to connect to database: ", err)
	}

	// ── WhatsApp ──────────────────────────────────────────────────────────────
	var waMgr *config.WAManager
	if waEnabled {
		waMgr, err = config.InitWA(*dbAddr)
		if err != nil {
			// Non-fatal: log and continue without WhatsApp.
			log.Printf("⚠️  WhatsApp init failed: %v", err)
		}
	}

	// ── Redis ─────────────────────────────────────────────────────────────────
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		log.Fatal("❌ REDIS_ADDR not set")
	}
	redisPass := os.Getenv("REDIS_PASSWORD")
	if redisPass == "" {
		log.Fatal("❌ REDIS_PASSWORD not set")
	}
	redisClient := config.InitRedisDB(redisAddr, redisPass, 0)

	// ── JWT ───────────────────────────────────────────────────────────────────
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("❌ JWT_SECRET not set")
	}
	if len(jwtSecret) < 32 {
		log.Fatal("❌ JWT_SECRET must be at least 32 characters")
	}

	// ── Repositories ─────────────────────────────────────────────────────────
	authRepo := repository.NewAuthRepository(db)
	studentRepo := repository.NewStudentRepository(db)
	teacherRepo := repository.NewTeacherRepository(db)
	managerRepo := repository.NewManagerRepository(db)
	adminRepo := repository.NewAdminRepository(db)
	otpRepo := repository.NewOTPRedisRepository(redisClient)
	paymentRepo := repository.NewPaymentRepository(db)
	teacherPaymentRepo := repository.NewTeacherPaymentRepository(db)
	reportRepo := repository.NewReportRepository(db)
	financeRepo := repository.NewFinanceRepository(db)

	// ── Services ──────────────────────────────────────────────────────────────
	// All services that previously accepted *whatsmeow.Client now accept
	// *config.WAManager (nil-safe — they guard with IsLoggedIn() checks).
	studentService := service.NewStudentUseCase(studentRepo, waMgr)
	managementService := service.NewManagerService(managerRepo, waMgr)
	adminService := service.NewAdminService(adminRepo, waMgr)
	teacherService := service.NewTeacherService(teacherRepo, waMgr)
	authService := service.NewAuthService(authRepo, otpRepo, jwtSecret)
	paymentService := service.NewPaymentService(paymentRepo, adminRepo, db, waMgr)
	teacherPaymentService := service.NewTeacherPaymentService(teacherPaymentRepo, adminRepo)
	reportService := service.NewReportService(reportRepo)
	financeService := service.NewFinanceService(financeRepo)

	// ── Rate limiter ──────────────────────────────────────────────────────────
	if limiterEnabled {
		middleware.InitRateLimiter(redisClient)
	}

	// ── Gin ───────────────────────────────────────────────────────────────────
	app := gin.Default()
	config.InitMiddleware(app, authService.GetAccessTokenManager())

	// ── Handlers ─────────────────────────────────────────────────────────────
	delivery.NewAuthHandler(app, authService, db)
	delivery.NewManagerHandler(app, managementService, authService.GetAccessTokenManager(), db)
	delivery.NewStudentHandler(app, studentService, authService.GetAccessTokenManager())
	delivery.NewAdminHandler(app, adminService, authService.GetAccessTokenManager())
	delivery.NewTeacherHandler(app, teacherService, authService.GetAccessTokenManager(), db)
	delivery.NewPaymentHandler(app, paymentService, authService.GetAccessTokenManager())
	delivery.NewTeacherPaymentHandler(app, teacherPaymentService, authService.GetAccessTokenManager(), db)
	delivery.NewReportHandler(app, reportService, authService.GetAccessTokenManager())
	delivery.NewFinanceHandler(app, financeService, paymentService, authService.GetAccessTokenManager(), db)
	delivery.NewUploadHandler(app, authService.GetAccessTokenManager())

	c := InitCron(teacherPaymentService, db, waMgr)

	return app, db, c
}

func InitializeFullApp() (*gin.Engine, *gorm.DB, *cron.Cron) {
	return initApp(true, true)
}

func InitializeAppWithoutWhatsappNotification() (*gin.Engine, *gorm.DB, *cron.Cron) {
	return initApp(false, true)
}

func InitializeAppWithoutRateLimiter() (*gin.Engine, *gorm.DB, *cron.Cron) {
	return initApp(true, false)
}