package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// NormalizeEmail is the deployment-global identity normalization used by the
// owner preflight and owner bootstrap. PostgreSQL still enforces lower(email)
// uniqueness; this helper makes the proposed owner ID reproducible before the
// owner row exists.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ProposedOwnerID deterministically derives the permanent owner ID from the
// normalized owner email. This lets the pre-migration dry run name the exact
// stable ID that owner setup will create and that legacy threads will receive.
func ProposedOwnerID(email string) string {
	sum := sha256.Sum256([]byte("agentbox:deployment-owner:" + NormalizeEmail(email)))
	return "usr_" + hex.EncodeToString(sum[:16])
}
