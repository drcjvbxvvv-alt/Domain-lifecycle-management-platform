package artifact

import "errors"

var (
	ErrArtifactNotFound      = errors.New("artifact not found")
	ErrChecksumMismatch      = errors.New("artifact checksum mismatch")
	ErrSignatureInvalid      = errors.New("artifact signature invalid")
	ErrTemplateNotPublished  = errors.New("template version not published")
	ErrNoDomainsInScope      = errors.New("no active domains in build scope")
	ErrArtifactImmutable     = errors.New("artifact is signed and immutable")
)
