package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"domain-platform/api/middleware"
	"domain-platform/internal/auth"
	"domain-platform/store/postgres"
)

type AuthHandler struct {
	authSvc *auth.Service
	users   *postgres.UserStore
	roles   *postgres.RoleStore
	logger  *zap.Logger
}

func NewAuthHandler(authSvc *auth.Service, users *postgres.UserStore, roles *postgres.RoleStore, logger *zap.Logger) *AuthHandler {
	return &AuthHandler{authSvc: authSvc, users: users, roles: roles, logger: logger}
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login handles POST /api/v1/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40000, "data": nil, "message": "invalid request: username and password required",
		})
		return
	}

	result, err := h.authSvc.Login(c.Request.Context(), req.Username, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 40100, "data": nil, "message": "invalid username or password",
		})
		return
	}
	if errors.Is(err, auth.ErrUserDisabled) {
		c.JSON(http.StatusForbidden, gin.H{
			"code": 40300, "data": nil, "message": "account is disabled",
		})
		return
	}
	if err != nil {
		h.logger.Error("login error", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "ok",
		"data": gin.H{
			"token":    result.Token,
			"user_id":  result.UserID,
			"username": result.Username,
			"roles":    result.Roles,
		},
	})
}

// Me handles GET /api/v1/auth/me — returns the current user's info from JWT claims.
func (h *AuthHandler) Me(c *gin.Context) {
	userID := middleware.GetUserID(c)
	user, err := h.users.GetByID(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("me: get user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	roles, err := h.roles.GetUserRoles(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("me: get roles", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 50000, "data": nil, "message": "internal error",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "ok",
		"data": gin.H{
			"user_id":      user.ID,
			"uuid":         user.UUID,
			"username":     user.Username,
			"display_name": user.DisplayName,
			"status":       user.Status,
			"roles":        roles,
			"created_at":   user.CreatedAt,
		},
	})
}
