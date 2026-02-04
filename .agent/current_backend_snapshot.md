# Backend Snapshot - PRODUCTION VERSION
> **Tanggal**: 2026-01-31 02:22 WIB
> **Tujuan**: Catatan file backend saat ini (sudah dikonfigurasi untuk production) sebelum di-replace dengan clone dari teman.

---

## 📁 Struktur File Backend

```
backend/
├── .env (119 lines)
├── app/
├── config/
│   ├── db.go (251 lines)
│   ├── gin.go (191 lines)
│   ├── redis.go
│   └── whatsapp.go
├── delivery/
│   ├── admin.go (659 lines)
│   ├── auth.go (513 lines) ⭐ PENTING: Konfigurasi Cookie Production
│   ├── manager.go (147 lines)
│   ├── student.go (333 lines)
│   └── teacher.go (612 lines)
├── middleware/
│   ├── middleware.go (146 lines)
│   ├── rate_limiter.go
│   └── ratelimiter_whitelist.go
├── utils/
│   ├── jwt.go (59 lines)
│   └── ... (8 files total)
└── ... (domain, dto, repository, service)
```

---

## ⭐ FILE PENTING YANG SUDAH DIKONFIGURASI PRODUCTION

### 1. `.env` - Environment Variables (119 lines)
**Variabel Production yang sudah ditambahkan:**
- `TARGETED_DOMAIN = https://www.madeu.app/`
- `APP_NAME=MadEU`
- `SUPPORT_PHONE=089696767876`
- `QR_CODE_EMAIL_RECEIVER = dognub61@gmail.com`

```env
ALLOW_ORIGINS=http://localhost:3000,http://localhost:5173,https://yourdomain.com
APP_ENV=production
```

---

### 2. `delivery/auth.go` - Cross-Domain Cookie Configuration (513 lines)
**CRITICAL PRODUCTION CODE (Line 16-46):**

```go
// Helper function to set cross-domain cookie
// Uses SameSite=None and Secure=true for cross-origin requests (Vercel ↔ Heroku)
func setRefreshTokenCookie(c *gin.Context, token string, maxAge int) {
    // Check if we're in production (HTTPS)
    isProduction := os.Getenv("APP_ENV") == "production" || os.Getenv("PORT") != ""

    if isProduction {
        // Production: Use SameSite=None for cross-domain
        c.SetSameSite(http.SameSiteNoneMode)
        c.SetCookie(
            "refresh_token",
            token,
            maxAge,
            "/",
            "",   // empty domain = current domain
            true, // Secure = true (required for SameSite=None)
            true, // HttpOnly
        )
    } else {
        // Development: Use default SameSite
        c.SetCookie(
            "refresh_token",
            token,
            maxAge,
            "/",
            "",
            false, // Secure = false for localhost
            true,  // HttpOnly
        )
    }
}
```

**Login Handler (Line 389-418) - Cross-Domain Support:**
```go
// ✅ Also return refresh_token in body for cross-domain deployment
// Cookie tidak bekerja untuk cross-domain (Vercel ↔ Heroku)
c.JSON(http.StatusOK, gin.H{
    "success":       true,
    "access_token":  tokens.AccessToken,
    "refresh_token": tokens.RefreshToken, // ✅ ADDED for cross-domain support
    "message":       "Login successful",
})
```

---

### 3. `config/gin.go` - CORS & Middleware Configuration (191 lines)
**CORS Production Configuration (Line 24-38):**

```go
corsOrigins := os.Getenv("ALLOW_ORIGINS")
if corsOrigins == "" {
    corsOrigins = "http://localhost:3000"
    log.Println("⚠️  ALLOW_ORIGINS not set, using default: http://localhost:3000")
}

app.Use(cors.New(cors.Config{
    AllowOrigins:     strings.Split(corsOrigins, ","),
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
    AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
    ExposeHeaders:    []string{"Content-Length"},
    AllowCredentials: true,
    MaxAge:           12 * time.Hour,
}))
```

**Security Headers (Line 69-72):**
```go
// Only set HSTS in production with HTTPS
if os.Getenv("APP_ENV") == "production" {
    c.Writer.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
}
```

---

### 4. `config/db.go` - Database Configuration (251 lines)
**Logger Mode Based on Environment (Line 44-49):**
```go
var gormLogger logger.Interface
if os.Getenv("APP_ENV") == "development" {
    gormLogger = logger.Default.LogMode(logger.Info)
} else {
    gormLogger = logger.Default.LogMode(logger.Silent)
}
```

**Connection Pool Production Settings (Line 70-74):**
```go
sqlDB.SetMaxIdleConns(10)
sqlDB.SetMaxOpenConns(100)
sqlDB.SetConnMaxLifetime(time.Hour)
sqlDB.SetConnMaxIdleTime(30 * time.Minute)
```

---

### 5. `delivery/teacher.go` - Timezone Configuration (612 lines)
**WITA Timezone for Schedule (Line 373-377):**
```go
// Get your local timezone (WITA)
loc, err := time.LoadLocation("Asia/Makassar") // WITA timezone
if err != nil {
    loc = time.FixedZone("WITA", 8*60*60) // UTC+8 as fallback
}
```

---

### 6. `middleware/middleware.go` - Role Middleware (146 lines)
**Middleware untuk role-based access:**
- `AdminOnly()` - Line 13-28
- `TeacherAndAdminOnly()` - Line 30-45
- `StudentAndAdminOnly()` - Line 47-62
- `ManagerAndAdminOnly()` - Line 64-90
- `ValidateTurnedOffUserMiddleware()` - Line 92-145

---

### 7. `utils/jwt.go` - JWT Token Manager (59 lines)
**Token Generation (Line 24-35):**
```go
func (j *JWTManager) GenerateToken(userUUID string, role, name string) (string, error) {
    claims := jwt.MapClaims{
        "sub":  userUUID,
        "name": name,
        "role": role,
        "exp":  time.Now().Add(j.tokenDuration).Unix(),
        "iat":  time.Now().Unix(),
    }

    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(j.secretKey)
}
```

---

## 🔑 KEY PRODUCTION MODIFICATIONS SUMMARY

| File | Line(s) | Modification |
|------|---------|--------------|
| `delivery/auth.go` | 16-46 | Cross-domain cookie (`SameSite=None`, `Secure=true`) |
| `delivery/auth.go` | 400-406 | Return `refresh_token` in response body |
| `config/gin.go` | 24-38 | CORS dengan `AllowCredentials: true` |
| `config/gin.go` | 69-72 | HSTS header hanya di production |
| `config/db.go` | 44-49 | Silent logger di production |
| `config/db.go` | 70-74 | Connection pool settings |
| `delivery/teacher.go` | 373-377 | WITA timezone (`Asia/Makassar`) |
| `.env` | 43-44 | `TARGETED_DOMAIN` |
| `.env` | 63 | `APP_NAME` |
| `.env` | 91-95 | `SUPPORT_PHONE`, `QR_CODE_EMAIL_RECEIVER` |

---

## ⚠️ CATATAN PENTING

Ketika file di-replace dengan clone dari teman:
1. **Cek `delivery/auth.go`** - Pastikan fungsi `setRefreshTokenCookie()` masih ada dengan konfigurasi production
2. **Cek `.env`** - Pastikan variabel production seperti `TARGETED_DOMAIN`, `APP_NAME`, dll masih ada
3. **Cek `config/gin.go`** - Pastikan CORS masih dikonfigurasi benar
4. **Cek timezone** - Pastikan masih menggunakan `Asia/Makassar` (WITA)

---

## 📋 CHECKLIST SETELAH REPLACE

- [ ] `delivery/auth.go` - Cookie cross-domain
- [ ] `delivery/auth.go` - Return refresh_token in body
- [ ] `config/gin.go` - CORS AllowCredentials
- [ ] `config/db.go` - Silent logger production
- [ ] `.env` - Semua variabel production
- [ ] `delivery/teacher.go` - WITA timezone
