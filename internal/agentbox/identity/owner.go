package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// NormalizeEmail is the deployment-global identity normalization used by owner
// bootstrap. PostgreSQL also enforces lower(email) uniqueness.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// OwnerIDForEmail deterministically derives the permanent owner ID from the
// normalized owner email so repeated owner bootstrap resolves to one stable ID.
func OwnerIDForEmail(email string) string {
	sum := sha256.Sum256([]byte("agentbox:deployment-owner:" + NormalizeEmail(email)))
	return "usr_" + hex.EncodeToString(sum[:16])
}
