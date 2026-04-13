package delivery

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/middleware"
	"chronosphere/utils"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TeacherPaymentHandler struct {
	uc domain.TeacherPaymentUseCase
	db *gorm.DB
}

func NewTeacherPaymentHandler(app *gin.Engine, uc domain.TeacherPaymentUseCase, jwtManager *utils.JWTManager, db *gorm.DB) {
	h := &TeacherPaymentHandler{uc: uc, db: db}

	// ─────────────────────────────────────────────────────────────────────────────
	// SHARED / "ME" ENDPOINT
	// ─────────────────────────────────────────────────────────────────────────────
	meGroup := app.Group("/finance/teacher-payments")
	meGroup.Use(config.AuthMiddleware(jwtManager), middleware.TeacherOnly(), middleware.ValidateTurnedOffUserMiddleware(db))
	{
		// GET /finance/teacher-payments/me?status=unpaid
		// Accessible by any authenticated user to see their own teacher payments.
		meGroup.GET("/me", h.GetMyPayments)
	}

	financeAndManager := app.Group("/finance/teacher-payments")
	financeAndManager.Use(config.AuthMiddleware(jwtManager), middleware.FinanceAndManagerOnly())
	{
		// GET /admin/teacher-payments?status=unpaid
		// status: unpaid | paid | (empty = all)
		financeAndManager.GET("", h.GetAllPayments)
	}

	managerOnly := app.Group("/finance/teacher-payments")
	managerOnly.Use(config.AuthMiddleware(jwtManager), middleware.ManagerAndAdminOnly(), middleware.ValidateTurnedOffUserMiddleware(db))
	{
		// POST /admin/teacher-payments/generate?year=2025&month=1
		// Calculates earnings for all teachers for the given month and inserts
		// unpaid payment records. Idempotent — safe to call multiple times.
		managerOnly.POST("/generate", h.GenerateMonthlyPayments)

		// GET /admin/teacher-payments/teacher/:uuid?status=unpaid
		// Payment history for a specific teacher.
		managerOnly.GET("/teacher/:uuid", h.GetPaymentsByTeacher)

		// PUT /admin/teacher-payments/:id/mark-paid
		// Body: { proof_image_url, notes }
		managerOnly.PUT("/:id/mark-paid", h.MarkAsPaid)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GenerateMonthlyPayments
// POST /admin/teacher-payments/generate?year=2025&month=1
// ─────────────────────────────────────────────────────────────────────────────

func (h *TeacherPaymentHandler) GenerateMonthlyPayments(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	yearStr := c.Query("year")
	monthStr := c.Query("month")

	if yearStr == "" || monthStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "query parameter 'year' dan 'month' wajib diisi",
			"message": "Gagal membuat data pembayaran",
		})
		return
	}

	year, err := strconv.Atoi(yearStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "format year tidak valid",
			"message": "Gagal membuat data pembayaran",
		})
		return
	}

	month, err := strconv.Atoi(monthStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "format month tidak valid",
			"message": "Gagal membuat data pembayaran",
		})
		return
	}

	details, err := h.uc.GenerateMonthlyPayments(c.Request.Context(), year, month)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GenerateMonthlyPayments - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal membuat data pembayaran bulanan",
		})
		return
	}

	utils.PrintLogInfo(&name, 201, "GenerateMonthlyPayments", nil)
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Data pembayaran bulanan berhasil dibuat",
		"data":    details,
		"total":   len(details),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetAllPayments
// GET /admin/teacher-payments?status=unpaid
// ─────────────────────────────────────────────────────────────────────────────

func (h *TeacherPaymentHandler) GetAllPayments(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	status := c.Query("status") // unpaid | paid | "" (all)

	payments, err := h.uc.GetAllPayments(c.Request.Context(), status)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetAllPayments - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil data pembayaran",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetAllPayments", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    payments,
		"total":   len(payments),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetPaymentsByTeacher
// GET /admin/teacher-payments/teacher/:uuid
// ─────────────────────────────────────────────────────────────────────────────

func (h *TeacherPaymentHandler) GetPaymentsByTeacher(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	teacherUUID := c.Param("uuid")
	status := c.Query("status") // unpaid | paid | "" (all)

	payments, err := h.uc.GetPaymentsByTeacher(c.Request.Context(), teacherUUID, status)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetPaymentsByTeacher - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil riwayat pembayaran guru",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetPaymentsByTeacher", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    payments,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetMyPayments
// GET /finance/teacher-payments/me?status=unpaid
// ─────────────────────────────────────────────────────────────────────────────

func (h *TeacherPaymentHandler) GetMyPayments(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	userUUID, exists := c.Get("userUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "konteks pengguna tidak ditemukan",
			"message": "Gagal mengambil data pembayaran",
		})
		return
	}

	status := c.Query("status") // unpaid | paid | "" (all)

	payments, err := h.uc.GetPaymentsByTeacher(c.Request.Context(), userUUID.(string), status)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetMyPayments - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil data pembayaran Anda",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetMyPayments", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    payments,
		"total":   len(payments),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// MarkAsPaid
// PUT /admin/teacher-payments/:id/mark-paid
// Body: { "proof_image_url": "https://...", "notes": "optional" }
// ─────────────────────────────────────────────────────────────────────────────

func (h *TeacherPaymentHandler) MarkAsPaid(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	adminUUID, exists := c.Get("userUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "konteks pengguna tidak ditemukan",
			"message": "Gagal menandai pembayaran",
		})
		return
	}

	idStr := c.Param("id")
	paymentID, err := strconv.Atoi(idStr)
	if err != nil || paymentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "payment ID tidak valid",
			"message": "Gagal menandai pembayaran",
		})
		return
	}

	var req domain.MarkPaidRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "MarkAsPaid - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal menandai pembayaran",
		})
		return
	}

	if err := h.uc.MarkAsPaid(c.Request.Context(), paymentID, adminUUID.(string), req); err != nil {
		utils.PrintLogInfo(&name, 500, "MarkAsPaid - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal menandai pembayaran",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "MarkAsPaid", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Pembayaran berhasil ditandai sebagai lunas",
	})
}
