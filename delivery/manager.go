package delivery

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/dto"
	"chronosphere/middleware"
	"chronosphere/utils"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ManagerHandler struct {
	uc domain.ManagerUseCase
}

func NewManagerHandler(app *gin.Engine, uc domain.ManagerUseCase, jwtManager *utils.JWTManager, db *gorm.DB) {
	h := &ManagerHandler{uc: uc}

	manager := app.Group("/manager")
	manager.Use(config.AuthMiddleware(jwtManager), middleware.ManagerOnly(), middleware.ValidateTurnedOffUserMiddleware(db))
	{
		manager.GET("/teachers/:uuid/schedules", h.GetTeacherSchedules)
		manager.GET("/teachers", h.GetAllTeachers)
		manager.GET("/students", h.GetAllStudents)
		manager.POST("/students", h.CreateStudent)
		manager.PUT("/students/:uuid/packages/:package_id/quota", h.ModifyStudentPackageQuota)
		manager.PUT("/modify", h.UpdateManager)
		manager.PUT("/modify/student/:uuid", h.UpdateStudent)
		manager.GET("/booked-classes", h.GetAllBookedClasses)
		manager.PUT("/booked-classes/:id/cancel", h.CancelBookedClass)

		manager.GET("/class-histories/cancelled", h.GetCancelledClassHistories)
		manager.POST("/rebook", h.RebookWithSubstitute)
	}

	financeAndManager := app.Group("/manager")
	financeAndManager.Use(config.AuthMiddleware(jwtManager), middleware.FinanceAndManagerOnly(), middleware.ValidateTurnedOffUserMiddleware(db))
	{
		financeAndManager.GET("/settings", h.GetSetting)
		financeAndManager.PUT("/settings", h.UpdateSetting)
		financeAndManager.GET("/students/:uuid", h.GetStudentByUUID)
	}
}

func (h *ManagerHandler) GetAllBookedClasses(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	bookings, err := h.uc.GetAllBookedClasses(c.Request.Context())
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetAllBookedClasses - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil data kelas",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetAllBookedClasses", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    bookings,
		"total":   len(*bookings),
		"message": "Data kelas berhasil diambil",
	})
}

func (h *ManagerHandler) CancelBookedClass(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	managerUUID, exists := c.Get("userUUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "konteks pengguna tidak ditemukan",
		})
		return
	}

	bookingID, err := strconv.Atoi(c.Param("id"))
	if err != nil || bookingID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "ID booking tidak valid",
			"message": "Gagal membatalkan kelas",
		})
		return
	}

	var req dto.CancelBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		utils.PrintLogInfo(&name, 400, "CancelBookedClass - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "Body permintaan tidak valid",
			"message": "Gagal membatalkan kelas",
		})
		return
	}

	if req.Reason != nil && len(*req.Reason) == 0 {
		req.Reason = nil
	}

	if err := h.uc.CancelBookedClass(c.Request.Context(), bookingID, managerUUID.(string), req.Reason); err != nil {
		utils.PrintLogInfo(&name, 500, "CancelBookedClass - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal membatalkan kelas",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "CancelBookedClass", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Kelas berhasil dibatalkan dan kuota telah dikembalikan",
	})
}

func (h *ManagerHandler) GetTeacherSchedules(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	teacherUUID := c.Param("uuid")

	requiredDuration := 0
	if d := c.Query("required_duration"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil {
			requiredDuration = parsed
		}
	}

	schedules, err := h.uc.GetTeacherSchedules(c.Request.Context(), teacherUUID, requiredDuration)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetTeacherSchedules - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetTeacherSchedules", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": schedules})
}

func (h *ManagerHandler) GetAllTeachers(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	teacherUUID := c.Query("teacherUUID")
	teachers, err := h.uc.GetAllTeachers(c.Request.Context(), teacherUUID)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetAllTeachers - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil data guru",
		})
		return
	}
	utils.PrintLogInfo(&name, 200, "GetAllTeachers", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    teachers,
		"total":   len(teachers),
		"message": "Data guru berhasil diambil",
	})
}

func (h *ManagerHandler) GetCancelledClassHistories(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	histories, err := h.uc.GetCancelledClassHistories(c.Request.Context())
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetCancelledClassHistories - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengambil riwayat kelas yang dibatalkan",
		})
		return
	}
	utils.PrintLogInfo(&name, 200, "GetCancelledClassHistories", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    histories,
		"message": "Riwayat kelas yang dibatalkan berhasil diambil",
	})
}

func (h *ManagerHandler) RebookWithSubstitute(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	var req dto.RebookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "RebookWithSubstitute - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal membuat ulang pemesanan",
		})
		return
	}

	// asia makassar timezone
	loc, _ := time.LoadLocation("Asia/Makassar")
	classDate := time.Now().In(loc)

	input := domain.RebookInput{
		OriginalBookingID: req.OriginalBookingID,
		SubScheduleID:     req.SubScheduleID,
		ClassDate:         classDate,
	}

	booking, err := h.uc.RebookWithSubstitute(c.Request.Context(), input)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "RebookWithSubstitute - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal membuat ulang pemesanan",
		})
		return
	}

	utils.PrintLogInfo(&name, 201, "RebookWithSubstitute", nil)
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    booking,
		"message": "Kelas berhasil dialihkan ke guru pengganti",
	})
}

func (h *ManagerHandler) GetSetting(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	setting, err := h.uc.GetSetting(c.Request.Context())
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetSetting - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error(), "message": "Gagal mengambil pengaturan"})
		return
	}
	utils.PrintLogInfo(&name, 200, "GetSetting", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": setting, "message": "Pengaturan berhasil diambil"})
}

func (h *ManagerHandler) UpdateSetting(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	var req UpdateSettingRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "UpdateSetting - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{"message": "Gagal memperbarui pengaturan", "success": false, "error": utils.TranslateValidationError(err)})
		return
	}

	setting := &domain.Setting{
		RegistrationFee:   req.RegistrationFee,
		TeacherCommission: req.TeacherCommission,
	}

	if err := h.uc.UpdateSetting(c.Request.Context(), setting); err != nil {
		utils.PrintLogInfo(&name, 500, "UpdateSetting - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error(), "message": "Gagal memperbarui pengaturan"})
		return
	}

	utils.PrintLogInfo(&name, 200, "UpdateSetting", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Pengaturan berhasil diperbarui"})
}

func (h *ManagerHandler) UpdateStudent(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	uuid := c.Param("uuid")
	var req dto.ManagerUpdateStudentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "UpdateStudent - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal mengubah data siswa",
		})
		return
	}
	user := dto.MapUpdateStudentRequest(&req)
	user.UUID = uuid
	// Pass password separately so service can hash it only when present
	user.Password = req.Password

	if err := h.uc.UpdateStudent(c.Request.Context(), user); err != nil {
		utils.PrintLogInfo(&name, 500, "UpdateStudent - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   utils.TranslateDBError(err),
			"message": "Gagal mengubah data siswa",
		})
		return
	}
	utils.PrintLogInfo(&name, 200, "UpdateStudent", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Data siswa berhasil diubah",
	})
}

func (h *ManagerHandler) UpdateManager(c *gin.Context) {
	managerName := utils.GetAPIHitter(c)
	userUUID, exists := c.Get("userUUID")
	if !exists {
		utils.PrintLogInfo(&managerName, 401, "UpdateManager", nil)
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "Tidak terotorisasi: konteks pengguna tidak ditemukan",
			"message": "Gagal memperbarui profil manajer",
		})
		return
	}
	var req dto.UpdateManagerRequest
	req.UUID = userUUID.(string)
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&managerName, 400, "UpdateManager - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"massage": "Gagal memperbarui profil manajer",
		})

		return
	}

	defaultImage := os.Getenv("DEFAULT_PROFILE_IMAGE")
	if req.Image == "" {
		req.Image = defaultImage
	}

	user := dto.MakeUpdateManagerRequest(&req)
	user.UUID = userUUID.(string) // assign dari URL, bukan dari JSON
	if err := h.uc.UpdateManager(c.Request.Context(), user); err != nil {
		utils.PrintLogInfo(&managerName, 500, "UpdateManager - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   utils.TranslateDBError(err),
			"message": "Gagal memperbarui profil manajer",
		})
		return
	}
	utils.PrintLogInfo(&managerName, 200, "UpdateManager", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Profil manajer berhasil diperbarui",
	})
}

func (h *ManagerHandler) GetAllStudents(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	students, err := h.uc.GetAllStudents(c.Request.Context())
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetAllStudents - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error(), "message": "Gagal mengambil data siswa"})
		return
	}
	utils.PrintLogInfo(&name, 200, "GetAllStudents", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": students, "message": "Data siswa berhasil diambil"})
}

func (h *ManagerHandler) CreateStudent(c *gin.Context) {
	var req dto.CreateStudentRequest // pakai DTO
	adminName := utils.GetAPIHitter(c)

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&adminName, 400, "CreateStudent - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"massage": "Gagal membuat siswa",
		})
		return
	}

	user := dto.MapCreateStudentRequestToUser(&req)

	created, err := h.uc.CreateStudent(c.Request.Context(), user)
	if err != nil {
		utils.PrintLogInfo(&adminName, 500, "CreateStudent - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   utils.TranslateDBError(err),
			"massage": "Gagal membuat siswa",
		})
		return
	}

	utils.PrintLogInfo(&adminName, 201, "CreateStudent", nil)
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    created,
		"message": "Siswa berhasil dibuat",
	})
}

func (h *ManagerHandler) GetStudentByUUID(c *gin.Context) {
	name := utils.GetAPIHitter(c)
	uuid := c.Param("uuid")
	student, err := h.uc.GetStudentByUUID(c.Request.Context(), uuid)
	if err != nil {
		utils.PrintLogInfo(&name, 500, "GetStudentByUUID - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error(), "message": "Gagal mengambil data siswa"})
		return
	}

	utils.PrintLogInfo(&name, 200, "GetStudentByUUID", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": student, "message": "Data siswa berhasil diambil"})
}

func (h *ManagerHandler) ModifyStudentPackageQuota(c *gin.Context) {
	name := utils.GetAPIHitter(c)

	studentUUID := c.Param("uuid")
	packageID, err := strconv.Atoi(c.Param("package_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Gagal mengubah kuota siswa", "error": "ID paket tidak valid"})
		return
	}

	var req struct {
		IncomingQuota int `json:"incoming_quota" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.PrintLogInfo(&name, 400, "ModifyStudentPackageQuota - BindJSON", &err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   utils.TranslateValidationError(err),
			"message": "Gagal mengubah kuota siswa",
		})
		return
	}

	if err := h.uc.ModifyStudentPackageQuota(c.Request.Context(), studentUUID, packageID, req.IncomingQuota); err != nil {
		utils.PrintLogInfo(&name, 500, "ModifyStudentPackageQuota - UseCase", &err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
			"message": "Gagal mengubah kuota siswa",
		})
		return
	}

	utils.PrintLogInfo(&name, 200, "ModifyStudentPackageQuota", nil)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Kuota paket berhasil diubah",
	})
}
