package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Context keys for agent identity set by AgentMTLS middleware.
const (
	CtxKeyAgentCertSerial = "agent_cert_serial"
	CtxKeyAgentCertCN     = "agent_cert_cn"
)

// AgentMTLS extracts the client certificate from the TLS connection and
// sets the cert serial and common name in the Gin context.
//
// In production, the TLS listener is configured with RequireAndVerifyClientCert
// (see cmd/server/main.go buildAgentTLSConfig), so the cert has already been
// validated against the Agent CA by the time this middleware runs.
//
// In dev mode (no TLS), this middleware is permissive and allows requests
// without client certs, setting placeholder values for testing.
func AgentMTLS() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.TLS != nil && len(c.Request.TLS.PeerCertificates) > 0 {
			cert := c.Request.TLS.PeerCertificates[0]
			c.Set(CtxKeyAgentCertSerial, cert.SerialNumber.String())
			c.Set(CtxKeyAgentCertCN, cert.Subject.CommonName)
			c.Next()
			return
		}

		// Dev mode: no TLS or no client cert — allow through with placeholder
		// Production mTLS enforcement is handled at the TLS layer, not middleware
		c.Set(CtxKeyAgentCertSerial, "dev-no-cert")
		c.Set(CtxKeyAgentCertCN, "dev-agent")
		c.Next()
	}
}

// GetAgentCertSerial extracts the agent cert serial from the Gin context.
func GetAgentCertSerial(c *gin.Context) string {
	v, _ := c.Get(CtxKeyAgentCertSerial)
	s, _ := v.(string)
	return s
}

// GetAgentCertCN extracts the agent cert common name from the Gin context.
func GetAgentCertCN(c *gin.Context) string {
	v, _ := c.Get(CtxKeyAgentCertCN)
	s, _ := v.(string)
	return s
}

// RequireAgentCert rejects requests that don't have a valid client certificate.
// Use this in production to enforce mTLS at the middleware level as a defense-in-depth
// measure on top of the TLS RequireAndVerifyClientCert config.
func RequireAgentCert() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.TLS == nil || len(c.Request.TLS.PeerCertificates) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": 40100, "data": nil, "message": "client certificate required",
			})
			return
		}
		c.Next()
	}
}
