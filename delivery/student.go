package delivery

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/dto"
	"chronosphere/middleware"
	"chronosphere/utils"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type StudentHandler struct {
	studUC domain.StudentUseCase
}

func NewStudentHandler(r *gin.Engine, studUC domain.StudentUseCase, jwtManager *utils.JWTManager) {
	handler := &StudentHandler{studUC: studUC}

	r.GET("/packages", middleware.OptionalAuthMiddleware(jwtManager), handler.GetAllAvailablePackages)
	// Public instruments endpoint — no auth required (instrument list is non-sensitive)
	r.GET("/instruments", handler.GetAllInstruments)

	student := r.Group("/student")
	student.Use(config.AuthMiddleware(jwtManager), middleware.StudentOnly())
	{
		student.GET("/profile", handler.GetMyProfile)
		student.POST("/book", handler.BookClass)
		student.POST("/book/trial", handler.BookClassTrial) // NEW
		student.GET("/booked", handler.GetMyBookedClasses)
		student.GET("/classes", handler.GetAvailableSchedules)
		student.GET("/classes/trial", handler.GetAvailableSchedulesTrial) // NEW
		student.PUT("/modify", handler.UpdateStudentData)
		student.DELETE("/cancel/:booking_id", handler.CancelBookedClass)
		student.GET("/class-history", handler.GetMyClassHistory)
		student.GET("/teacher-details/:teacher_uuid", handler.GetTeacherDetails)

		// Bulk-book flow:
		// 1. See teacher schedules that match your package
		// 2. Preview proposed class dates (dry-run)
		// 3. Confirm and book all at once
		student.GET("/teacher-schedules", handler.GetTeacherSchedulesForPackage)
		student.POST("/bulk-book/preview", handler.BulkBookPreview)
		student.POST("/bulk-book", handler.BulkBookClass)

		// new
		// Inside NewStudentHandler, add inside the student group:
		student.GET("/instruments", handler.GetAllInstruments)
	}
}

func (h *StudentHandler) BookClassTrial(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&name, 401, "BookClassTrial", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "Tidak terotorisasi: konteks pengguna tidak ditemukan",
			"message": "Gagal memesan kelas trial",
		})
		return
	}

	var payload dto.BookClassTrialRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		utils.PrintLogInfo(&name, 400, "BookClassTrial", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal memesan kelas trial",
		})
		return
	}

	if _, err := h.studUC.BookClassTrial(
		c.Request.Context(),
		userUUID.(string),
		payload.ScheduleID,
		payload.PackageID,
		payload.InstrumentID,
	); err != nil {
		utils.PrintLogInfo(&name, 400, "BookClassTrial", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal memesan kelas trial",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "BookClassTrial", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Kelas trial berhasil dipesan"})
}

func (h *StudentHandler) GetAllInstruments(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	instruments, err := h.studUC.GetAllInstruments(c.Request.Context())
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetAllInstruments", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil daftar instrumen",
		})
		return
	}
	utils.PrintLogInfo(&name, 200, "GetAllInstruments", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    instruments,
		"total":   len(instruments),
	})
}

// GetAvailableSchedulesTrial returns all active teacher schedules for trial package browsing.
// Query param: package_id (required) — must be a trial student_packages.id owned by the student.
func (h *StudentHandler) GetAvailableSchedulesTrial(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&name, 401, "GetAvailableSchedulesTrial", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "Tidak terotorisasi: konteks pengguna tidak ditemukan",
			"message": "Gagal mengambil jadwal trial",
		})
		return
	}

	packageIDStr := c.Query("package_id")
	if packageIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "package_id wajib diisi",
			"message": "Gagal mengambil jadwal trial",
		})
		return
	}

	packageID, err := strconv.Atoi(packageIDStr)
	if err != nil || packageID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "package_id tidak valid",
			"message": "Gagal mengambil jadwal trial",
		})
		return
	}

	instrumentIDStr := c.Query("instrument_id")
	if instrumentIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "instrument_id wajib diisi",
			"message": "Gagal mengambil jadwal trial",
		})
		return
	}

	instrumentID, err := strconv.Atoi(instrumentIDStr)
	if err != nil || instrumentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "instrument_id tidak valid",
			"message": "Gagal mengambil jadwal trial",
		})
		return
	}

	schedules, err := h.studUC.GetAvailableSchedulesTrial(c.Request.Context(), userUUID.(string), packageID, instrumentID)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetAvailableSchedulesTrial", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil jadwal trial",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetAvailableSchedulesTrial", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": schedules})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetTeacherDetails
// ─────────────────────────────────────────────────────────────────────────────

func (h *StudentHandler) GetTeacherDetails(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	teacherUUID := c.Param("teacher_uuid")

	teacherDetails, err := h.studUC.GetTeacherDetails(c.Request.Context(), teacherUUID)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetTeacherDetails", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil detail guru",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetTeacherDetails", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": teacherDetails})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetMyClassHistory
// ─────────────────────────────────────────────────────────────────────────────

func (h *StudentHandler) GetMyClassHistory(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&name, 401, "GetMyClassHistory", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "Tidak terotorisasi: konteks pengguna tidak ditemukan",
			"message": "Gagal mengambil riwayat kelas saya",
		})
		return
	}

	f := parsePaginationStudent(c)
	histories, err := h.studUC.GetMyClassHistory(c.Request.Context(), userUUID.(string), f)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetMyClassHistory", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil riwayat kelas saya",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetMyClassHistory", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    histories,
		"page":    f.Page,
		"limit":   f.Limit,
		"total":   len(*histories),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// CancelBookedClass
// ─────────────────────────────────────────────────────────────────────────────

func (h *StudentHandler) CancelBookedClass(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&name, 401, "CancelBookedClass", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "Tidak terotorisasi: konteks pengguna tidak ditemukan",
			"message": "Gagal membatalkan kelas yang dipesan",
		})
		return
	}

	bookid := c.Param("booking_id")
	convertedID, err := strconv.Atoi(bookid)
	if err != nil {
		utils.PrintLogInfo(&name, 400, "CancelBookedClass", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Parameter ID pemesanan tidak valid",
			"message": "Gagal membatalkan kelas yang dipesan",
		})
		return
	}

	var req dto.CancelBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		utils.PrintLogInfo(&name, 400, "CancelBookedClass", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Body permintaan tidak valid",
			"message": "Gagal membatalkan kelas yang dipesan",
		})
		return
	}

	// After binding the CancelBookingRequest:
	if req.Reason != nil {
		normalized := strings.ReplaceAll(*req.Reason, `\n`, "\n")
		req.Reason = &normalized
	}

	if req.Reason != nil && len(*req.Reason) == 0 {
		req.Reason = nil
	}

	if err := h.studUC.CancelBookedClass(c.Request.Context(), convertedID, userUUID.(string), req.Reason); err != nil {
		utils.PrintLogInfo(&name, 500, "CancelBookedClass", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal membatalkan kelas yang dipesan",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "CancelBookedClass", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Kelas yang dipesan berhasil dibatalkan"})
}

// ─────────────────────────────────────────────────────────────────────────────
// BookClass
//
// Request body: { schedule_id, package_id, instrument_id }
//   - package_id: the student_packages.id to use (must belong to the student and be active).
//   - instrument_id: which instrument to study (required; for trial packages the student
//     picks the instrument because their trial package is instrument-agnostic).
// ─────────────────────────────────────────────────────────────────────────────

func (h *StudentHandler) BookClass(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&name, 401, "BookClass", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "Tidak terotorisasi: konteks pengguna tidak ditemukan",
			"message": "Gagal memesan kelas",
		})
		return
	}

	var payload dto.BookClassRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		utils.PrintLogInfo(&name, 400, "BookClass", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal memesan kelas",
		})
		return
	}

	if _, err := h.studUC.BookClass(
		c.Request.Context(),
		userUUID.(string),
		payload.ScheduleID,
		payload.InstrumentID,
	); err != nil {
		utils.PrintLogInfo(&name, 400, "BookClass", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal memesan kelas",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "BookClass", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Kelas berhasil dipesan"})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetAvailableSchedules
//
// Query param: package_id (required) — the student_packages.id the student intends to use.
// Response includes:
//   - All relevant teacher schedules enriched with availability flags.
//   - teacher_finished_class_count per schedule for frontend performance sorting.
//
// Trial packages: all active teacher schedules are returned (no instrument/duration filter).
// ─────────────────────────────────────────────────────────────────────────────

func (h *StudentHandler) GetAvailableSchedules(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&name, 401, "GetAvailableSchedules", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "Tidak terotorisasi: konteks pengguna tidak ditemukan",
			"message": "Gagal mengambil jadwal tersedia",
		})
		return
	}

	packageIDStr := c.Query("instrument_id")
	if packageIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "instrument_id wajib diisi",
			"message": "Gagal mengambil jadwal tersedia",
		})
		return
	}

	packageID, err := strconv.Atoi(packageIDStr)
	if err != nil || packageID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "package_id tidak valid",
			"message": "Gagal mengambil jadwal tersedia",
		})
		return
	}

	schedules, err := h.studUC.GetAvailableSchedules(c.Request.Context(), userUUID.(string), packageID)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetAvailableSchedules", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil jadwal tersedia",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetAvailableSchedules", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": schedules})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetMyBookedClasses
// ─────────────────────────────────────────────────────────────────────────────

func (h *StudentHandler) GetMyBookedClasses(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&name, 401, "GetMyBookedClasses", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "Tidak terotorisasi: konteks pengguna tidak ditemukan",
			"message": "Gagal mengambil kelas yang dipesan",
		})
		return
	}

	f := parsePaginationStudent(c)
	bookings, err := h.studUC.GetMyBookedClasses(c.Request.Context(), userUUID.(string), f)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetMyBookedClasses", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil kelas yang dipesan",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetMyBookedClasses", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    bookings,
		"page":    f.Page,
		"limit":   f.Limit,
		"total":   len(*bookings),
	})
}

// parsePaginationStudent reads ?page=&limit= query params.
// Defaults: page=1, limit=10. limit=0 means fetch all.
func parsePaginationStudent(c *gin.Context) domain.PaginationFilter {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 0 {
		limit = 10
	}
	return domain.PaginationFilter{Page: page, Limit: limit}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetAllAvailablePackages
// ─────────────────────────────────────────────────────────────────────────────

// delivery/student.go
func (h *StudentHandler) GetAllAvailablePackages(c *gin.Context) {
	var studentUUID *string
	if uid, exists := c.Get("userUUID"); exists {
		s := uid.(string)
		studentUUID = &s
	}

	packages, setting, err := h.studUC.GetAllAvailablePackages(c.Request.Context(), studentUUID)
	if err != nil {
		utils.PrintLogInfo(nil, 500, "GetAllAvailablePackages", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil paket tersedia",
		})
		return
	}

	type returnModifiedResponse struct {
		Packages      []domain.Package `json:"packages"`
		Setting       domain.Setting   `json:"setting"`
		AdminWANumber string           `json:"admin_wa_number"` // NEW
		WAAvailable   bool             `json:"wa_available"`    // NEW
	}

	adminPhone := h.studUC.GetAdminWhatsAppNumber()

	res := returnModifiedResponse{
		Packages:      *packages,
		Setting:       *setting,
		AdminWANumber: adminPhone,
		WAAvailable:   adminPhone != "",
	}

	utils.PrintLogInfo(nil, 200, "GetAllAvailablePackages", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": res})
}

// ─────────────────────────────────────────────────────────────────────────────
// GetMyProfile
// ─────────────────────────────────────────────────────────────────────────────

func (h *StudentHandler) GetMyProfile(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&name, 401, "GetMyProfile", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "Tidak terotorisasi: konteks pengguna tidak ditemukan",
			"message": "Gagal mengambil profil saya",
		})
		return
	}

	user, err := h.studUC.GetMyProfile(c.Request.Context(), userUUID.(string))
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetMyProfile", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil profil saya",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetMyProfile", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": user})
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateStudentData
// ─────────────────────────────────────────────────────────────────────────────

func (h *StudentHandler) UpdateStudentData(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&name, 401, "UpdateStudentData", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "Tidak terotorisasi: konteks pengguna tidak ditemukan",
			"message": "Gagal memperbarui data siswa",
		})
		return
	}

	var payload dto.UpdateStudentDataRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		utils.PrintLogInfo(&name, 400, "UpdateStudentData", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Payload permintaan tidak valid",
		})
		return
	}

	filteredPayload := dto.MapUpdateStudentRequestByStudent(&payload)
	if err := h.studUC.UpdateStudentData(c.Request.Context(), userUUID.(string), filteredPayload); err != nil {
		utils.PrintLogInfo(&name, 500, "UpdateStudentData", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal memperbarui data siswa",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "UpdateStudentData", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Data siswa berhasil diperbarui"})
}

// ─────────────────────────────────────────────────────────────────────────────
// Bulk-Book Handlers
// ─────────────────────────────────────────────────────────────────────────────

// GetTeacherSchedulesForPackage GET /student/teacher-schedules?teacher_uuid=X&student_package_id=Y
// Returns the given teacher's schedules whose duration matches the student's package.
func (h *StudentHandler) GetTeacherSchedulesForPackage(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Tidak terotorisasi"})
		return
	}

	teacherUUID := c.Query("teacher_uuid")
	if teacherUUID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "teacher_uuid wajib diisi"})
		return
	}

	packageIDStr := c.Query("student_package_id")
	packageID, err := strconv.Atoi(packageIDStr)
	if err != nil || packageID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "student_package_id tidak valid"})
		return
	}

	schedules, err := h.studUC.GetTeacherSchedulesForPackage(c.Request.Context(), teacherUUID, packageID, userUUID.(string))
	if err != nil {
		utils.PrintLogInfo(&name, 400, "GetTeacherSchedulesForPackage", &err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetTeacherSchedulesForPackage", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": schedules, "total": len(*schedules)})
}

// BulkBookPreview POST /student/bulk-book/preview
// Dry-run: shows which dates would be booked without writing to the database.
func (h *StudentHandler) BulkBookPreview(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Tidak terotorisasi"})
		return
	}

	var req dto.BulkBookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "BulkBookPreview - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": utils.TranslateValidationError(err)})
		return
	}

	previews, err := h.studUC.BulkBookPreview(c.Request.Context(), userUUID.(string), req.StudentPackageID, req.ScheduleIDs)
	if err != nil {
		utils.PrintLogInfo(&name, 400, "BulkBookPreview - UseCase", &err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	utils.PrintLogInfo(&name, 200, "BulkBookPreview", nil)
	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"total_classes": len(previews),
		"preview":       previews,
	})
}

// BulkBookClass POST /student/bulk-book
// Commits all sessions at once and deducts the full package quota.
func (h *StudentHandler) BulkBookClass(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Tidak terotorisasi"})
		return
	}

	var req dto.BulkBookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "BulkBookClass - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": utils.TranslateValidationError(err)})
		return
	}

	result, err := h.studUC.BulkBookClass(c.Request.Context(), userUUID.(string), req.StudentPackageID, req.ScheduleIDs)
	if err != nil {
		utils.PrintLogInfo(&name, 400, "BulkBookClass - UseCase", &err)
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	utils.PrintLogInfo(&name, 201, "BulkBookClass", nil)
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": fmt.Sprintf("%d kelas berhasil dipesan", result.TotalBooked),
		"data":    result,
	})
}
