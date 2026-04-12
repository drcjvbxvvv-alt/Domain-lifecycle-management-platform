package release

import "errors"

var (
	ErrReleaseNotFound       = errors.New("release not found")
	ErrInvalidReleaseState   = errors.New("invalid release state transition")
	ErrReleaseRaceCondition  = errors.New("release state race condition")
	ErrDomainNotActive       = errors.New("domain is not in active state")
	ErrNoDomainsInScope      = errors.New("no active domains in release scope")
	ErrTemplateNotPublished  = errors.New("template version not published")
	ErrReleaseAlreadyStarted = errors.New("release already started")
	ErrRollbackNotAllowed    = errors.New("rollback not allowed from current release state")
	ErrNoPreviousRelease     = errors.New("no previous succeeded release found to roll back to")
)
