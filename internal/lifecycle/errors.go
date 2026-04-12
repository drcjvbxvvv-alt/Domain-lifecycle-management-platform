package lifecycle

import "errors"

var (
	// ErrDomainNotFound is returned when a domain ID does not exist or is soft-deleted.
	ErrDomainNotFound = errors.New("domain not found")

	// ErrInvalidLifecycleState is returned when a requested state transition
	// is not present in the validLifecycleTransitions map.
	ErrInvalidLifecycleState = errors.New("invalid domain lifecycle transition")

	// ErrLifecycleRaceCondition is returned when a concurrent Transition() call
	// has already moved the domain out of the expected `from` state. The caller
	// should retry or inform the user that the state has changed.
	ErrLifecycleRaceCondition = errors.New("domain lifecycle race condition: state changed concurrently")

	// ErrDuplicateFQDN is returned when attempting to register a domain with
	// an FQDN that already exists.
	ErrDuplicateFQDN = errors.New("domain fqdn already exists")
)
