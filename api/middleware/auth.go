package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"domain-platform/internal/auth"
)

// Context keys for user info set by JWTAuth middleware.
const (
	CtxKeyUserID   = "user_id"
	CtxKeyUsername = "username"
	CtxKeyRoles    = "roles"
)

// JWTAuth extracts and verifies the Bearer token from the Authorization header.
// On success it sets user_id, username, and roles in the Gin context.
func JWTAuth(jwtMgr *auth.JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": 40100, "data": nil, "message": "missing authorization header",
			})
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": 40100, "data": nil, "message": "invalid authorization format",
			})
			return
		}

		claims, err := jwtMgr.Verify(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": 40101, "data": nil, "message": "invalid or expired token",
			})
			return
		}

		c.Set(CtxKeyUserID, claims.UserID)
		c.Set(CtxKeyUsername, claims.Username)
		c.Set(CtxKeyRoles, claims.Roles)
		c.Next()
	}
}

// GetUserID extracts the user ID from the Gin context (set by JWTAuth).
func GetUserID(c *gin.Context) int64 {
	v, _ := c.Get(CtxKeyUserID)
	id, _ := v.(int64)
	return id
}

// GetRoles extracts the user's roles from the Gin context (set by JWTAuth).
func GetRoles(c *gin.Context) []string {
	v, _ := c.Get(CtxKeyRoles)
	roles, _ := v.([]string)
	return roles
}
