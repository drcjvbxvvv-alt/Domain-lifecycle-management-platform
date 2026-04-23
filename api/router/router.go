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
	CostHandler        *handler.CostHandler
	TagHandler         *handler.TagHandler
	ExpiryHandler      *handler.ExpiryHandler
	ImportHandler      *handler.ImportHandler
	DNSQueryHandler          *handler.DNSQueryHandler
	DNSRecordHandler         *handler.DNSRecordHandler
	DomainPermissionHandler  *handler.DomainPermissionHandler
	PermissionChecker        middleware.DNSPermissionChecker
	DNSTemplateHandler       *handler.DNSTemplateHandler
	ProbeHandler             *handler.ProbeHandler
	AlertHandler             *handler.AlertHandler
	JWTManager               *auth.JWTManager
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

		// ── Dashboard ─────────────────────────────────────────────────
		dashboard := authed.Group("/dashboard")
		{
			dashboard.GET("/expiry", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ExpiryHandler.Dashboard)
		}

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
			domains.POST("/bulk", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.TagHandler.BulkAction)
			domains.GET("/export", middleware.RequireAnyRole("operator", "release_manager", "admin", "auditor"), deps.TagHandler.Export)
			// Import routes (static, before /:id)
			domains.POST("/import", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.ImportHandler.Upload)
			domains.POST("/import/preview", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.ImportHandler.Preview)
			domains.GET("/import/jobs", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ImportHandler.ListJobs)
			domains.GET("/import/jobs/:jobid", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ImportHandler.GetJob)
			// Parameterized routes
			domains.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DomainHandler.Get)
			domains.PUT("/:id", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.UpdateAsset)
			domains.POST("/:id/transition", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.Transition)
			domains.GET("/:id/history", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DomainHandler.History)
			// Transfer tracking
			domains.POST("/:id/transfer", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.InitiateTransfer)
			domains.POST("/:id/transfer/complete", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.CompleteTransfer)
			domains.POST("/:id/transfer/cancel", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DomainHandler.CancelTransfer)
			// Domain tags
			domains.GET("/:id/tags", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.TagHandler.GetDomainTags)
			domains.PUT("/:id/tags", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.TagHandler.SetDomainTags)
			// DNS record lookup (live query)
			domains.GET("/:id/dns-records", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DNSQueryHandler.LookupByDomain)
			domains.GET("/:id/dns-propagation", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DNSQueryHandler.PropagationByDomain)
			domains.GET("/:id/dns-drift", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DNSQueryHandler.DriftCheck)
			// DNS record management via provider API (zone-level RBAC)
			domains.GET("/:id/provider-records", middleware.RequireDNSPermission(deps.PermissionChecker, "viewer"), deps.DNSRecordHandler.ListRecords)
			domains.POST("/:id/provider-records", middleware.RequireDNSPermission(deps.PermissionChecker, "editor"), deps.DNSRecordHandler.CreateRecord)
			domains.PUT("/:id/provider-records/:rid", middleware.RequireDNSPermission(deps.PermissionChecker, "editor"), deps.DNSRecordHandler.UpdateRecord)
			domains.DELETE("/:id/provider-records/:rid", middleware.RequireDNSPermission(deps.PermissionChecker, "editor"), deps.DNSRecordHandler.DeleteRecord)
			// Domain permissions (zone-level RBAC management)
			domains.GET("/:id/my-permission", deps.DomainPermissionHandler.MyPermission)
			domains.GET("/:id/permissions", middleware.RequireDNSPermission(deps.PermissionChecker, "viewer"), deps.DomainPermissionHandler.List)
			domains.POST("/:id/permissions", middleware.RequireDNSPermission(deps.PermissionChecker, "admin"), deps.DomainPermissionHandler.Grant)
			domains.DELETE("/:id/permissions/:user_id", middleware.RequireDNSPermission(deps.PermissionChecker, "admin"), deps.DomainPermissionHandler.Revoke)
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

		// ── DNS Record Templates ───────────────────────────────────────
		dnsTemplates := authed.Group("/dns-templates")
		{
			dnsTemplates.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DNSTemplateHandler.List)
			dnsTemplates.POST("", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DNSTemplateHandler.Create)
			dnsTemplates.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DNSTemplateHandler.Get)
			dnsTemplates.PUT("/:id", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DNSTemplateHandler.Update)
			dnsTemplates.DELETE("/:id", middleware.RequireAnyRole("admin"), deps.DNSTemplateHandler.Delete)
		}
		// Apply template (nested under domains)
		domains.POST("/:id/dns/apply-template", middleware.RequireDNSPermission(deps.PermissionChecker, "editor"), deps.DNSTemplateHandler.ApplyTemplate)

		// ── Tags ───────────────────────────────────────────────────────
		tags := authed.Group("/tags")
		{
			tags.POST("", middleware.RequireAnyRole("admin"), deps.TagHandler.Create)
			tags.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.TagHandler.List)
			tags.PUT("/:id", middleware.RequireAnyRole("admin"), deps.TagHandler.Update)
			tags.DELETE("/:id", middleware.RequireAnyRole("admin"), deps.TagHandler.Delete)
		}

		// ── Fee Schedules ──────────────────────────────────────────────
		feeSchedules := authed.Group("/fee-schedules")
		{
			feeSchedules.POST("", middleware.RequireAnyRole("admin"), deps.CostHandler.CreateFeeSchedule)
			feeSchedules.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.CostHandler.ListFeeSchedules)
			feeSchedules.PUT("/:id", middleware.RequireAnyRole("admin"), deps.CostHandler.UpdateFeeSchedule)
			feeSchedules.DELETE("/:id", middleware.RequireAnyRole("admin"), deps.CostHandler.DeleteFeeSchedule)
		}

		// ── Cost Records (nested under domains) ────────────────────────
		domains.POST("/:id/costs", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.CostHandler.CreateDomainCost)
		domains.GET("/:id/costs", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.CostHandler.ListDomainCosts)

		// ── Cost Summary ───────────────────────────────────────────────
		// Static path: /costs/summary — registered here (not under domains).
		costs := authed.Group("/costs")
		{
			costs.GET("/summary", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.CostHandler.GetCostSummary)
		}

		// ── DNS Lookup (arbitrary FQDN) ───────────────────────────
		dnsLookup := authed.Group("/dns")
		{
			dnsLookup.GET("/lookup", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DNSQueryHandler.LookupByFQDN)
			dnsLookup.GET("/propagation", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.DNSQueryHandler.PropagationByFQDN)
			dnsLookup.POST("/drift-check-all", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.DNSQueryHandler.TriggerDriftCheckAll)
		}

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

		// ── Probe Policies (PC.1) ──────────────────────────────────────
		probePolicies := authed.Group("/probe-policies")
		{
			probePolicies.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ProbeHandler.ListPolicies)
			probePolicies.POST("", middleware.RequireAnyRole("admin"), deps.ProbeHandler.CreatePolicy)
			probePolicies.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ProbeHandler.GetPolicy)
			probePolicies.PUT("/:id", middleware.RequireAnyRole("admin"), deps.ProbeHandler.UpdatePolicy)
			probePolicies.DELETE("/:id", middleware.RequireAnyRole("admin"), deps.ProbeHandler.DeletePolicy)
		}

		// Probe results nested under domains
		domains.GET("/:id/probe-results", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.ProbeHandler.ListDomainResults)

		// ── Alerts (PC.2) ─────────────────────────────────────────────────
		alerts := authed.Group("/alerts")
		{
			alerts.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.AlertHandler.ListAlerts)
			alerts.GET("/summary", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.AlertHandler.AlertSummary)
			alerts.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.AlertHandler.GetAlert)
			alerts.POST("/:id/resolve", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.AlertHandler.ResolveAlert)
			alerts.POST("/:id/acknowledge", middleware.RequireAnyRole("operator", "release_manager", "admin"), deps.AlertHandler.AcknowledgeAlert)
		}

		// ── Notification Rules (PC.2) ─────────────────────────────────────
		notifRules := authed.Group("/notification-rules")
		{
			notifRules.GET("", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.AlertHandler.ListRules)
			notifRules.POST("", middleware.RequireAnyRole("admin"), deps.AlertHandler.CreateRule)
			notifRules.GET("/:id", middleware.RequireAnyRole("viewer", "operator", "release_manager", "admin", "auditor"), deps.AlertHandler.GetRule)
			notifRules.PUT("/:id", middleware.RequireAnyRole("admin"), deps.AlertHandler.UpdateRule)
			notifRules.DELETE("/:id", middleware.RequireAnyRole("admin"), deps.AlertHandler.DeleteRule)
		}
	}
}
