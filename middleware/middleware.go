package middleware

import (
	"chronosphere/domain"
	"chronosphere/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func FinanceOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := utils.GetAPIHitter(c)
		role, exists := c.Get("role")
		if !exists || role != domain.RoleFinance {
			utils.PrintLogInfo(&name, 403, "FinanceOnly Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses finance diperlukan",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// FinanceAndManagerOnly allows finance, manager, and admin roles to access
// financial reports. Admin retains full access as the super-role.
func FinanceAndManagerOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := utils.GetAPIHitter(c)
		role, exists := c.Get("role")
		if !exists {
			utils.PrintLogInfo(&name, 403, "FinanceAndManagerOnly Middleware - No Role", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses tidak diizinkan",
			})
			c.Abort()
			return
		}

		r, _ := role.(string)
		allowed := r == domain.RoleFinance ||
			r == domain.RoleManagement ||
			r == domain.RoleAdmin

		if !allowed {
			utils.PrintLogInfo(&name, 403, "FinanceAndManagerOnly Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses finance, manajer, atau admin diperlukan",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// role checking middleware
func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := utils.GetAPIHitter(c)
		role, exists := c.Get("role")
		if !exists || role != domain.RoleAdmin {
			utils.PrintLogInfo(&name, 403, "AdminOnly Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses admin diperlukan",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func TeacherAndAdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := utils.GetAPIHitter(c)
		role, exists := c.Get("role")
		if !exists || role == domain.RoleStudent {
			utils.PrintLogInfo(&name, 403, "Admin and Teacher only Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses admin dan guru diperlukan",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func StudentAndAdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := utils.GetAPIHitter(c)
		role, exists := c.Get("role")
		if !exists || role == domain.RoleTeacher {
			utils.PrintLogInfo(&name, 403, "Admin and Student only Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses admin dan siswa diperlukan",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func StudentOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := utils.GetAPIHitter(c)
		role, _ := c.Get("role")
		if role != domain.RoleStudent {
			utils.PrintLogInfo(&name, 403, "Student only Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses siswa diperlukan",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func ManagerAndAdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := utils.GetAPIHitter(c)
		role, exists := c.Get("role")
		if !exists {
			utils.PrintLogInfo(&name, 403, "Admin and Manager only Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses admin dan manajer diperlukan",
			})
			c.Abort()
			return
		}

		// Check if role is either Admin or Manager
		if role != domain.RoleAdmin && role != domain.RoleManagement {
			utils.PrintLogInfo(&name, 403, "Admin and Manager only Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses admin dan manajer diperlukan",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func ManagerOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := utils.GetAPIHitter(c)
		role, exists := c.Get("role")
		if !exists {
			utils.PrintLogInfo(&name, 403, "Manager only Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses manajer diperlukan",
			})
			c.Abort()
			return
		}

		// Check if role is either Admin or Manager
		if role != domain.RoleManagement {
			utils.PrintLogInfo(&name, 403, "Manager only Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses manajer diperlukan",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func TeacherOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := utils.GetAPIHitter(c)
		role, exists := c.Get("role")
		if !exists || role == domain.RoleStudent || role == domain.RoleManagement || role == domain.RoleAdmin {
			utils.PrintLogInfo(&name, 403, "Teacher only Middleware - Role Check", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses guru diperlukan",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// OptionalAuthMiddleware populates the user context if a valid Bearer token is present,
// but does NOT block the request if the token is missing or invalid.
// Use this on public routes that need to behave differently for authenticated users.
func OptionalAuthMiddleware(jwtManager *utils.JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.Next()
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		userUUID, role, name, err := jwtManager.VerifyToken(tokenStr)
		if err != nil {
			// Token present but invalid — still allow the request through
			c.Next()
			return
		}

		c.Set("userUUID", userUUID)
		c.Set("role", role)
		c.Set("name", name)
		c.Next()
	}
}

func ValidateTurnedOffUserMiddleware(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := utils.GetAPIHitter(c)
		role, exists := c.Get("role")
		if !exists {
			utils.PrintLogInfo(&name, 403, "Role Check Failure", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Pengecekan peran pengguna gagal / tidak ditemukan",
			})
			c.Abort()
			return
		}

		if role != domain.RoleTeacher && role != domain.RoleManagement && role != domain.RoleFinance {
			c.Next()
			return
		}

		userUUID, exists := c.Get("userUUID")
		if !exists {
			utils.PrintLogInfo(&name, 403, "User UUID checker failure", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "UUID pengguna tidak ditemukan",
			})
			c.Abort()
			return
		}

		var user domain.User
		err := db.Model(domain.User{}).Where("uuid = ?", userUUID.(string)).First(&user).Error
		if err != nil {
			utils.PrintLogInfo(&name, 500, "Database error when fetching user", &err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "Pengguna tidak ditemukan",
				"error":   err.Error(),
			})
			c.Abort()
			return
		}

		if user.DeletedAt != nil {
			utils.PrintLogInfo(&name, 403, "User account is turned off", nil)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"error":   "Akun anda telah dinonaktifkan, silakan hubungi admin untuk informasi lebih lanjut",
				"message": "Akun dinonaktifkan",
			})
			c.Abort()
			return
		}
	}
}
