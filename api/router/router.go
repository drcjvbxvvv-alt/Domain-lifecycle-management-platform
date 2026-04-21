package router

import (
	"github.com/gin-gonic/gin"

	"domain-platform/api/handler"
	"domain-platform/api/middleware"
	"domain-platform/internal/auth"
)

// Deps bundles all dependencies needed to register routes.
type Deps struct {
	AuthHandler        *handler.AuthHandler
	ProjectHandler     *handler.ProjectHandler
	DomainHandler      *handler.DomainHandler
	TemplateHandler    *handler.TemplateHandler
	ArtifactHandler    *handler.ArtifactHandler
	ReleaseHandler     *handler.ReleaseHandler
	AgentHandler       *handler.AgentHandler
	HostGroupHandler   *handler.HostGroupHandler
	RegistrarHandler   *handler.RegistrarHandler
	DNSProviderHandler *handler.DNSProviderHandler
	SSLHandler         *handler.SSLHandler
	JWTManager         *auth.JWTManager
}

// RegisterV1 mounts all /api/v1 routes onto the Gin engine.
func RegisterV1(r *gin.Engine, deps Deps) {
	v1 := r.Group("/api/v1")

	// ── Public routes (no auth) ────────────────���───────────────────────
	authGroup := v1.Group("/auth")
	{
		authGroup.POST("/login", deps.AuthHandler.Login)
	}

	// ── Authenticated routes ───────────────────────────────���───────────
	authed := v1.Group("", middleware.JWTAuth(deps.JWTManager))
	{
		authed.GET("/auth/me", deps.AuthHandler.Me)

		// ── Projects ──────────────────────────────────────────────────
		projects := authed.Group("/projects")
		{
			projects.POST("", middleware.RequireAnyRole("admin"), deps.ProjectHandler.Create)
			projects.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ProjectHandler.List)
			projects.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ProjectHandler.Get)
			projects.PUT("/:id", middleware.RequireAnyRole("admin"), deps.ProjectHandler.Update)
			projects.DELETE("/:id", middleware.RequireAnyRole("admin"), deps.ProjectHandler.Delete)
			// Template sub-routes scoped to a project
			projects.POST("/:id/templates", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.TemplateHandler.Create)
			projects.GET("/:id/templates", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.TemplateHandler.List)
			// Artifact sub-routes scoped to a project
			projects.GET("/:id/artifacts", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ArtifactHandler.ListByProject)
			// Release sub-routes scoped to a project
			projects.GET("/:id/releases", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ReleaseHandler.ListByProject)
		}

		// ── Domains ───────────────────────────────────────────────────
		// NOTE: static sub-paths (expiring, stats) MUST be registered before
		// the :id param route — otherwise Gin routes "expiring" as an ID.
		domains := authed.Group("/domains")
		{
			domains.POST("", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.Register)
			domains.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DomainHandler.List)
			// Static sub-paths (before /:id)
			domains.GET("/expiring", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DomainHandler.Expiring)
			domains.GET("/stats", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DomainHandler.Stats)
			// Parameterized routes
			domains.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DomainHandler.Get)
			domains.PUT("/:id", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.UpdateAsset)
			domains.POST("/:id/transition", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.Transition)
			domains.GET("/:id/history", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DomainHandler.History)
			// Transfer tracking
			domains.POST("/:id/transfer", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.InitiateTransfer)
			domains.POST("/:id/transfer/complete", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.CompleteTransfer)
			domains.POST("/:id/transfer/cancel", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.CancelTransfer)
		}

		// ── Templates (individual) ─────────────────────────────────────
		templates := authed.Group("/templates")
		{
			templates.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.TemplateHandler.Get)
			templates.PUT("/:id", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.TemplateHandler.Update)
			templates.DELETE("/:id", middleware.RequireAnyRole("admin"), deps.TemplateHandler.Delete)
			templates.POST("/:id/versions/publish", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.TemplateHandler.PublishVersion)
			templates.GET("/:id/versions", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.TemplateHandler.ListVersions)
		}

		// ── Template versions (individual) ────────────────────────────
		templateVersions := authed.Group("/template-versions")
		{
			templateVersions.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.TemplateHandler.GetVersion)
			templateVersions.PATCH("/:id", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.TemplateHandler.UpdateVersion)
		}

		// ── Artifacts (individual, read-only) ─────────────────────────
		artifacts := authed.Group("/artifacts")
		{
			artifacts.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ArtifactHandler.Get)
		}

		// ── Releases ──────────────────────────────────────────────────
		releases := authed.Group("/releases")
		{
			releases.POST("", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.ReleaseHandler.Create)
			releases.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ReleaseHandler.Get)
			releases.POST("/:id/pause", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.ReleaseHandler.Pause)
			releases.POST("/:id/resume", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.ReleaseHandler.Resume)
			releases.POST("/:id/cancel", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.ReleaseHandler.Cancel)
			releases.POST("/:id/rollback", middleware.RequireAnyRole("release_manager", "admin"), deps.ReleaseHandler.Rollback)
			releases.GET("/:id/history", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ReleaseHandler.History)
			releases.GET("/:id/dry-run", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ReleaseHandler.DryRun)
		}

		// ── Agents (management console, read + transition) ────────────
		agents := authed.Group("/agents")
		{
			agents.GET("", middleware.RequireAnyRole("operator", "release_manager", "admin", "auditor"), deps.AgentHandler.List)
			agents.GET("/:id", middleware.RequireAnyRole("operator", "release_manager", "admin", "auditor"), deps.AgentHandler.Get)
			agents.POST("/:id/transition", middleware.RequireAnyRole("admin"), deps.AgentHandler.Transition)
			agents.GET("/:id/history", middleware.RequireAnyRole("operator", "release_manager", "admin", "auditor"), deps.AgentHandler.History)
		}

		// ── Host Groups (concurrency + reload-batch settings) ─────────
		hostGroups := authed.Group("/host-groups")
		{
			hostGroups.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.HostGroupHandler.List)
			hostGroups.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.HostGroupHandler.Get)
			hostGroups.PUT("/:id", middleware.RequireAnyRole("admin"), deps.HostGroupHandler.UpdateConcurrency)
		}

		// ── Registrars ────────────────────────────────────────────────
		registrars := authed.Group("/registrars")
		{
			registrars.POST("", middleware.RequireAnyRole("admin"), deps.RegistrarHandler.Create)
			registrars.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.RegistrarHandler.List)
			registrars.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.RegistrarHandler.Get)
			registrars.PUT("/:id", middleware.RequireAnyRole("admin"), deps.RegistrarHandler.Update)
			registrars.DELETE("/:id", middleware.RequireAnyRole("admin"), deps.RegistrarHandler.Delete)
			// Registrar accounts (nested)
			registrars.POST("/:id/accounts", middleware.RequireAnyRole("admin"), deps.RegistrarHandler.CreateAccount)
			registrars.GET("/:id/accounts", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.RegistrarHandler.ListAccounts)
		}

		// ── Registrar Accounts (individual) ───────────────────────────
		registrarAccounts := authed.Group("/registrar-accounts")
		{
			registrarAccounts.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.RegistrarHandler.GetAccount)
			registrarAccounts.PUT("/:id", middleware.RequireAnyRole("admin"), deps.RegistrarHandler.UpdateAccount)
			registrarAccounts.DELETE("/:id", middleware.RequireAnyRole("admin"), deps.RegistrarHandler.DeleteAccount)
		}

		// ── SSL Certificates ──────────────────────────────────────────
		// NOTE: static path /expiring must be before /:id.
		sslCerts := authed.Group("/ssl-certs")
		{
			sslCerts.GET("/expiring", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.SSLHandler.ListExpiring)
			sslCerts.DELETE("/:id", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.SSLHandler.Delete)
		}
		// SSL sub-routes nested under domains
		domains.POST("/:id/ssl-certs", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.SSLHandler.Create)
		domains.GET("/:id/ssl-certs", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.SSLHandler.List)
		domains.POST("/:id/ssl-certs/check", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.SSLHandler.Check)

		// ── DNS Providers ─────────────────────────────────────────────
		dnsProviders := authed.Group("/dns-providers")
		{
			dnsProviders.GET("/types", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DNSProviderHandler.SupportedTypes)
			dnsProviders.POST("", middleware.RequireAnyRole("admin"), deps.DNSProviderHandler.Create)
			dnsProviders.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DNSProviderHandler.List)
			dnsProviders.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DNSProviderHandler.Get)
			dnsProviders.PUT("/:id", middleware.RequireAnyRole("admin"), deps.DNSProviderHandler.Update)
			dnsProviders.DELETE("/:id", middleware.RequireAnyRole("admin"), deps.DNSProviderHandler.Delete)
		}
	}
}
