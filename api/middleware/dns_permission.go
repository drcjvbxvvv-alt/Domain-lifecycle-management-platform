package middleware

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// DNSPermissionChecker is implemented by domain.PermissionService.
type DNSPermissionChecker interface {
	HasPermission(ctx context.Context, domainID, userID int64, minLevel string) (bool, error)
}

// RequireDNSPermission returns middleware that checks the caller's effective
// permission level on the target domain (resolved from the :id path param).
// Must be used after JWTAuth middleware.
//
// Permission levels (ordered): viewer < editor < admin
func RequireDNSPermission(checker DNSPermissionChecker, minLevel string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := GetUserID(c)
		if userID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": 40100, "data": nil, "message": "not authenticated",
			})
			return
		}

		rawID := c.Param("id")
		domainID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"code": 40000, "data": nil, "message": "invalid domain id",
			})
			return
		}

		ok, err := checker.HasPermission(c.Request.Context(), domainID, userID, minLevel)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"code": 50000, "data": nil, "message": "permission check failed",
			})
			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    40300,
				"data":    nil,
				"message": "insufficient domain permission (need " + minLevel + ")",
			})
			return
		}

		c.Next()
	}
}
