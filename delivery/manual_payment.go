package delivery

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/middleware"
	"chronosphere/utils"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ManualPaymentHandler wires up all manual-payment routes.
//
// Route overview:
//
//	Student:
//	  POST   /student/manual-payment              — request a manual payment
//	  GET    /student/manual-payment              — list own requests
//
//	Admin / Manager:
//	  GET    /admin/manual-payments               — list all (optional ?status=pending|confirmed|rejected)
//	  PUT    /admin/manual-payments/:id/confirm   — confirm + auto-assign package
//	  PUT    /admin/manual-payments/:id/reject    — reject
type ManualPaymentHandler struct {
	uc domain.ManualPaymentUseCase
}

func NewManualPaymentHandler(
	app *gin.Engine,
	uc domain.ManualPaymentUseCase,
	jwtManager *utils.JWTManager,
) {
	h := &ManualPaymentHandler{uc: uc}

	// ── Student routes ────────────────────────────────────────────────────────
	student := app.Group("/student/manual-payment")
	student.Use(config.AuthMiddleware(jwtManager), middleware.StudentOnly())
	{
		student.POST("", h.RequestManualPayment)
		student.GET("", h.GetMyManualPayments)
	}

	// ── Admin / Manager routes ────────────────────────────────────────────────
	admin := app.Group("/admin/manual-payments")
	admin.Use(config.AuthMiddleware(jwtManager), middleware.ManagerAndAdminOnly())
	{
		admin.GET("", h.GetAllManualPayments)
		admin.PUT("/:id/confirm", h.ConfirmManualPayment)
		admin.PUT("/:id/reject", h.RejectManualPayment)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// POST /student/manual-payment
// Body: { "package_id": 3 }
// ─────────────────────────────────────────────────────────────────────────────

type requestManualPaymentBody struct {
	PackageID int `json:"package_id" binding:"required,gt=0"`
}

func (h *ManualPaymentHandler) RequestManualPayment(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&name, 401, "RequestManualPayment", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "konteks pengguna tidak ditemukan",
			"message": "Gagal membuat permintaan pembayaran",
		})
		return
	}

	var req requestManualPaymentBody
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "RequestManualPayment - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal membuat permintaan pembayaran",
		})
		return
	}

	mp, err := h.uc.RequestManualPayment(c.Request.Context(), userUUID.(string), req.PackageID)
	if err != nil {
		utils.PrintLogInfo(&name, 400, "RequestManualPayment - UseCase", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal membuat permintaan pembayaran",
		})
		return
	}

	utils.PrintLogInfo(&name, 201, "RequestManualPayment", nil)
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Permintaan pembayaran berhasil dibuat. Admin akan segera menghubungi Anda.",
		"data":    mp,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /student/manual-payment
// ─────────────────────────────────────────────────────────────────────────────

func (h *ManualPaymentHandler) GetMyManualPayments(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	userUUID, exists := c.Get("userUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "konteks pengguna tidak ditemukan",
		})
		return
	}

	payments, err := h.uc.GetMyManualPayments(c.Request.Context(), userUUID.(string))
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetMyManualPayments - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil riwayat pembayaran manual",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetMyManualPayments", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    payments,
		"total":   len(payments),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /admin/manual-payments?status=pending
// ─────────────────────────────────────────────────────────────────────────────

func (h *ManualPaymentHandler) GetAllManualPayments(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	status := c.Query("status") // pending | confirmed | rejected | (empty = all)

	payments, err := h.uc.GetAllManualPayments(c.Request.Context(), status)
	if err != nil {
		utils.PrintLogInfo(&name, 400, "GetAllManualPayments - UseCase", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil daftar pembayaran manual",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetAllManualPayments", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    payments,
		"total":   len(payments),
		"filter":  status,
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// PUT /admin/manual-payments/:id/confirm
// Body: { "notes": "optional admin note" }
// ─────────────────────────────────────────────────────────────────────────────

func (h *ManualPaymentHandler) ConfirmManualPayment(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	adminUUID, exists := c.Get("userUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "konteks pengguna tidak ditemukan",
		})
		return
	}

	paymentID, err := parseIDParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "ID pembayaran tidak valid",
			"message": "Gagal mengkonfirmasi pembayaran",
		})
		return
	}

	var req domain.ManualPaymentConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "ConfirmManualPayment - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal mengkonfirmasi pembayaran",
		})
		return
	}

	if err := h.uc.ConfirmManualPayment(c.Request.Context(), paymentID, adminUUID.(string), req); err != nil {
		utils.PrintLogInfo(&name, 400, "ConfirmManualPayment - UseCase", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengkonfirmasi pembayaran",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "ConfirmManualPayment", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Pembayaran berhasil dikonfirmasi dan paket telah diaktifkan",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// PUT /admin/manual-payments/:id/reject
// Body: { "notes": "reason for rejection" }
// ─────────────────────────────────────────────────────────────────────────────

func (h *ManualPaymentHandler) RejectManualPayment(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	adminUUID, exists := c.Get("userUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "konteks pengguna tidak ditemukan",
		})
		return
	}

	paymentID, err := parseIDParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "ID pembayaran tidak valid",
			"message": "Gagal menolak pembayaran",
		})
		return
	}

	var req domain.ManualPaymentRejectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "RejectManualPayment - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal menolak pembayaran",
		})
		return
	}

	if err := h.uc.RejectManualPayment(c.Request.Context(), paymentID, adminUUID.(string), req); err != nil {
		utils.PrintLogInfo(&name, 400, "RejectManualPayment - UseCase", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal menolak pembayaran",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "RejectManualPayment", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Permintaan pembayaran berhasil ditolak",
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func parseIDParam(c *gin.Context, param string) (int, error) {
	id, err := strconv.Atoi(c.Param(param))
	if err != nil || id <= 0 {
		return 0, err
	}
	return id, nil
}