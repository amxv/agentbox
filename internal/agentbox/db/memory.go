package db

import (
	"context"
	"sort"
	"sync"
	"time"

	"agentbox/internal/agentbox/types"
)

type memoryAssetLease struct{ asset types.Asset }

func (l memoryAssetLease) Asset() types.Asset          { return l.asset }
func (l memoryAssetLease) Close(context.Context) error { return nil }

type memoryPublicThreadLease struct{ thread types.ThreadWithMessages }

func (l memoryPublicThreadLease) Thread() types.ThreadWithMessages { return l.thread }
func (l memoryPublicThreadLease) Close(context.Context) error      { return nil }

type memoryAttachmentPurgeLease struct {
	mutex *sync.Mutex
	once  sync.Once
}

func (l *memoryAttachmentPurgeLease) Close(context.Context) error {
	l.once.Do(func() { l.mutex.Unlock() })
	return nil
}

type MemoryRepository struct {
	purgeMutex        sync.Mutex
	Threads           []types.Thread
	Messages          []types.Message
	Assets            []types.Asset
	Pending           []types.PendingUpload
	UploadCleanup     []memoryUploadCleanup
	APIKeys           []types.APIKey
	Users             []types.User
	Sessions          []types.UserSession
	CLICodes          []types.CLILoginCode
	OwnerSetupTokens  []memoryOwnerSetupToken
	SignupInvitations []memorySignupInvitation
	Teams             []types.Team
	TeamMemberships   []types.TeamMembership
	ThreadTeamShares  []types.ThreadTeamShare
	ThreadPublicLinks []types.ThreadPublicLink
	Onboarding        []types.OnboardingState
	RaycastSetupURLs  map[string]string
}

type memoryOwnerSetupToken struct {
	Token     types.OwnerSetupToken
	TokenHash string
}

type memorySignupInvitation struct {
	Invitation types.SignupInvitation
	TokenHash  string
	TeamIDs    []string
}

type memoryUploadCleanup struct {
	Candidate    types.UploadCleanupCandidate
	NotBefore    time.Time
	CleanedAt    *time.Time
	AttemptCount int
	LastError    string
}

func messagePositionLess(left types.Message, right types.Message) bool {
	if left.Position > 0 && right.Position > 0 && left.Position != right.Position {
		return left.Position < right.Position
	}
	if left.CreatedAt != right.CreatedAt {
		return left.CreatedAt < right.CreatedAt
	}
	return left.ID < right.ID
}

func messagePositionAfter(left types.Message, right types.Message) bool {
	return messagePositionLess(right, left)
}

func assetPositionLess(left types.Asset, right types.Asset) bool {
	if left.Position > 0 && right.Position > 0 && left.Position != right.Position {
		return left.Position < right.Position
	}
	if left.CreatedAt != right.CreatedAt {
		return left.CreatedAt < right.CreatedAt
	}
	return left.ID < right.ID
}

func sortMessageAssets(message *types.Message) {
	sort.SliceStable(message.Assets, func(i, j int) bool {
		return assetPositionLess(message.Assets[i], message.Assets[j])
	})
}
