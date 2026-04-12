// Package audit provides append-only audit log writing for all state-changing operations.
// Every mutation (create/update/delete) performed by a user or the system
// is recorded to the audit_logs table via WriteAuditLog().
package audit
