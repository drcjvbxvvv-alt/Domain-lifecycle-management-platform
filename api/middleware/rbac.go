package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireRole returns middleware that requires the authenticated user to have
// the specified role. Must be used after JWTAuth middleware.
func RequireRole(role string) gin.HandlerFunc {
	return RequireAnyRole(role)
}

// RequireAnyRole returns middleware that requires the authenticated user to
// have at least one of the specified roles.
func RequireAnyRole(roles ...string) gin.HandlerFunc {
	roleSet := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		roleSet[r] = struct{}{}
	}

	return func(c *gin.Context) {
		userRoles := GetRoles(c)
		for _, r := range userRoles {
			if _, ok := roleSet[r]; ok {
				c.Next()
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"code":    40300,
			"data":    nil,
			"message": "insufficient permissions",
		})
	}
}
