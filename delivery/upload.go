package delivery

import (
	"chronosphere/config"
	"chronosphere/utils"
	"crypto/sha1"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

type UploadHandler struct{}

func NewUploadHandler(r *gin.Engine, jwtManager *utils.JWTManager) {
	handler := &UploadHandler{}

	protected := r.Group("/upload")
	protected.Use(config.AuthMiddleware(jwtManager))
	{
		protected.GET("/signature", handler.GenerateSignature)
	}
}

func (h *UploadHandler) GenerateSignature(c *gin.Context) {
	timestamp := time.Now().Unix()
	apiSecret := os.Getenv("CLOUDINARY_API_SECRET")

	if apiSecret == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "CLOUDINARY_API_SECRET is not configured",
		})
		return
	}

	// Format string yang diminta Cloudinary sebelum di-hash
	// We include folder=album_teacher because frontend needs it for Teacher Album
	// Note: All params must be sorted alphabetically by Cloudinary rules,
	// but since we only have 'folder' and 'timestamp', 'folder' (f) comes before 'timestamp' (t).
	dataToSign := fmt.Sprintf("folder=album_teacher&timestamp=%d%s", timestamp, apiSecret)

	// Hasilkan hash SHA-1
	hash := sha1.New()
	hash.Write([]byte(dataToSign))
	signature := fmt.Sprintf("%x", hash.Sum(nil))

	// Kembalikan ke frontend
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"timestamp": timestamp,
		"signature": signature,
	})
}
