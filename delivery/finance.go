package delivery

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/middleware"
	"chronosphere/utils"
	"math"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// FinanceHandler  — finance user management (admin-only CRUD)
//                   + financial report endpoints (finance, manager, admin)
// ─────────────────────────────────────────────────────────────────────────────

type FinanceHandler struct {
	financeUC domain.FinanceUseCase
	paymentUC domain.PaymentUseCase
}

// NewFinanceHandler registers all finance-related routes.
//
// Route overview:
//
//	Admin manages finance users:
//	  POST   /admin/finance                 — create finance user
//	  GET    /admin/finance                 — list all finance users
//	  GET    /admin/finance/:uuid           — get one finance user
//	  DELETE /admin/finance/:uuid           — soft-delete finance user
//
//	Finance user manages own profile:
//	  PUT    /finance/modify                — update own profile
//
//	Financial reports (finance + manager + admin):
//	  GET    /finance/payment/profit        — total profit
//	  GET    /finance/payment/history       — payment history (paginated)
//	  GET    /finance/payment/summary       — package sales summary
func NewFinanceHandler(
	app *gin.Engine,
	financeUC domain.FinanceUseCase,
	paymentUC domain.PaymentUseCase,
	jwtManager *utils.JWTManager,
) {
	h := &FinanceHandler{
		financeUC: financeUC,
		paymentUC: paymentUC,
	}

	// ── Admin: manage finance accounts ───────────────────────────────────────
	adminFinance := app.Group("/admin/finance")
	adminFinance.Use(config.AuthMiddleware(jwtManager), middleware.AdminOnly())
	{
		adminFinance.POST("", h.CreateFinance)
		adminFinance.GET("", h.GetAllFinance)
		adminFinance.GET("/:uuid", h.GetFinanceByUUID)
		adminFinance.DELETE("/:uuid", h.DeleteFinance)
	}

	// ── Finance user: own profile ─────────────────────────────────────────────
	financeProfile := app.Group("/finance")
	financeProfile.Use(config.AuthMiddleware(jwtManager), middleware.FinanceOnly())
	{
		financeProfile.PUT("/modify", h.UpdateFinance)
	}

	// ── Financial reports: finance + manager + admin ──────────────────────────
	// NOTE: The original endpoints at /admin/payment/* (AdminOnly) remain intact
	// in delivery/payment.go so admin access is NOT broken.
	// These new endpoints at /finance/payment/* open the same data to finance & manager.
	reports := app.Group("/finance/payment")
	reports.Use(config.AuthMiddleware(jwtManager), middleware.FinanceAndManagerOnly())
	{
		reports.GET("/profit", h.GetTotalProfit)
		reports.GET("/history", h.GetPaymentHistoryFinance)
		reports.GET("/summary", h.GetPackageSummary)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Admin: Create Finance User
// POST /admin/finance
// ─────────────────────────────────────────────────────────────────────────────

type createFinanceRequest struct {
	Name     string  `json:"name"     binding:"required,min=3,max=50"`
	Email    string  `json:"email"    binding:"required,email"`
	Phone    string  `json:"phone"    binding:"required,numeric,min=9,max=14"`
	Password string  `json:"password" binding:"required,min=8"`
	Gender   string  `json:"gender"   binding:"required,oneof=male female"`
	Image    *string `json:"image"    binding:"omitempty,url"`
}

func (h *FinanceHandler) CreateFinance(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	var req createFinanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "CreateFinance - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal membuat pengguna finance",
		})
		return
	}

	defImg := os.Getenv("DEFAULT_PROFILE_IMAGE")
	img := &defImg
	if req.Image != nil && *req.Image != "" {
		img = req.Image
	}

	user := &domain.User{
		Name:     req.Name,
		Email:    req.Email,
		Phone:    req.Phone,
		Password: req.Password,
		Gender:   req.Gender,
		Image:    img,
		Role:     domain.RoleFinance,
	}

	created, err := h.financeUC.CreateFinance(c.Request.Context(), user)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "CreateFinance - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal membuat pengguna finance",
		})
		return
	}

	utils.PrintLogInfo(&name, 201, "CreateFinance", nil)
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    created,
		"message": "Pengguna finance berhasil dibuat",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Admin: List all finance users
// GET /admin/finance
// ─────────────────────────────────────────────────────────────────────────────

func (h *FinanceHandler) GetAllFinance(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	users, err := h.financeUC.GetAllFinance(c.Request.Context())
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetAllFinance - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil daftar pengguna finance",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetAllFinance", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    users,
		"total":   len(users),
		"message": "Daftar pengguna finance berhasil diambil",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Admin: Get one finance user
// GET /admin/finance/:uuid
// ─────────────────────────────────────────────────────────────────────────────

func (h *FinanceHandler) GetFinanceByUUID(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	uuid := c.Param("uuid")

	user, err := h.financeUC.GetFinanceByUUID(c.Request.Context(), uuid)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetFinanceByUUID - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil data pengguna finance",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetFinanceByUUID", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    user,
		"message": "Data pengguna finance berhasil diambil",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Admin: Soft-delete finance user
// DELETE /admin/finance/:uuid
// ─────────────────────────────────────────────────────────────────────────────

func (h *FinanceHandler) DeleteFinance(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	uuid := c.Param("uuid")

	if err := h.financeUC.DeleteFinance(c.Request.Context(), uuid); err != nil {
		utils.PrintLogInfo(&name, 500, "DeleteFinance - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal menonaktifkan pengguna finance",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "DeleteFinance", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Pengguna finance berhasil dinonaktifkan",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Finance: Update own profile
// PUT /finance/modify
// ─────────────────────────────────────────────────────────────────────────────

type updateFinanceRequest struct {
	Name   string  `json:"name"   binding:"required,min=3,max=50"`
	Phone  string  `json:"phone"  binding:"required,numeric,min=9,max=14"`
	Gender string  `json:"gender" binding:"required,oneof=male female"`
	Image  *string `json:"image"  binding:"omitempty,url"`
}

func (h *FinanceHandler) UpdateFinance(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	uuidVal, exists := c.Get("userUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "konteks pengguna tidak ditemukan",
			"message": "Gagal memperbarui profil",
		})
		return
	}

	var req updateFinanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "UpdateFinance - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal memperbarui profil finance",
		})
		return
	}

	user := &domain.User{
		UUID:   uuidVal.(string),
		Name:   req.Name,
		Phone:  req.Phone,
		Gender: req.Gender,
		Image:  req.Image,
	}

	if err := h.financeUC.UpdateFinance(c.Request.Context(), user); err != nil {
		utils.PrintLogInfo(&name, 500, "UpdateFinance - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   utils.TranslateDBError(err),
			"message": "Gagal memperbarui profil finance",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "UpdateFinance", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Profil finance berhasil diperbarui",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Financial Reports  (finance + manager + admin)
// ─────────────────────────────────────────────────────────────────────────────

// GET /finance/payment/profit?start_date=&end_date=
func (h *FinanceHandler) GetTotalProfit(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	var filter domain.ProfitFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		utils.PrintLogInfo(&name, 400, "GetTotalProfit - BindQuery", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Parameter filter tidak valid",
			"message": "Gagal mengambil data profit",
		})
		return
	}

	total, err := h.paymentUC.GetTotalProfit(c.Request.Context(), filter)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetTotalProfit - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil data profit",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetTotalProfit", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"total_profit": total,
			"filter":       filter,
		},
	})
}

// GET /finance/payment/history?page=&limit=&start_date=&end_date=&status=
func (h *FinanceHandler) GetPaymentHistoryFinance(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	var filter domain.HistoryFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		utils.PrintLogInfo(&name, 400, "GetPaymentHistoryFinance - BindQuery", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Parameter filter tidak valid",
			"message": "Gagal mengambil riwayat pembayaran",
		})
		return
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 {
		filter.Limit = 10
	}

	payments, total, err := h.paymentUC.GetPaymentHistory(c.Request.Context(), filter)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetPaymentHistoryFinance - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil riwayat pembayaran",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetPaymentHistoryFinance", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    payments,
		"pagination": gin.H{
			"page":        filter.Page,
			"limit":       filter.Limit,
			"total":       total,
			"total_pages": int(math.Ceil(float64(total) / float64(filter.Limit))),
		},
	})
}

// GET /finance/payment/summary
func (h *FinanceHandler) GetPackageSummary(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	summaries, err := h.paymentUC.GetPackageSummary(c.Request.Context())
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetPackageSummary - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil ringkasan paket",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetPackageSummary", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    summaries,
	})
}