package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"upstream-balance/internal/store"
)

type AuthHandler struct {
	settings *store.SettingStore
	tokens   sync.Map
}

func NewAuthHandler(settings *store.SettingStore) *AuthHandler {
	return &AuthHandler{settings: settings}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password required"})
		return
	}

	adminUser, _ := h.settings.Get("admin_username")
	adminPwd, _ := h.settings.Get("admin_password")

	if adminPwd == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未配置管理员密码"})
		return
	}
	if (adminUser != "" && req.Username != adminUser) || req.Password != adminPwd {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	token := generateToken()
	h.tokens.Store(token, true)
	c.JSON(http.StatusOK, gin.H{"token": token})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		h.tokens.Delete(strings.TrimPrefix(auth, "Bearer "))
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *AuthHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		adminPwd, _ := h.settings.Get("admin_password")
		if adminPwd == "" {
			c.Next()
			return
		}

		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if _, ok := h.tokens.Load(token); !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.Next()
	}
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
