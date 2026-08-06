package types

import "strconv"

const DefaultOwnerPageLimit = 25
const MaxOwnerPageLimit = 100

type PageRequest struct {
	Limit  int
	Offset int
}

type PageInfo struct {
	Limit          int     `json:"limit"`
	Offset         int     `json:"offset"`
	HasMore        bool    `json:"has_more"`
	NextCursor     *string `json:"next_cursor,omitempty"`
	PreviousCursor *string `json:"previous_cursor,omitempty"`
}

func NormalizePageRequest(request PageRequest) PageRequest {
	if request.Limit <= 0 {
		request.Limit = DefaultOwnerPageLimit
	}
	if request.Limit > MaxOwnerPageLimit {
		request.Limit = MaxOwnerPageLimit
	}
	if request.Offset < 0 {
		request.Offset = 0
	}
	return request
}

// PageWindow converts a limit+1 query result into a bounded page. Cursors are
// continuation offsets rather than database identities; the collection order
// remains explicit and deterministic in each repository query.
func PageWindow(request PageRequest, fetched int) (visible int, info PageInfo) {
	request = NormalizePageRequest(request)
	visible = fetched
	if visible > request.Limit {
		visible = request.Limit
	}
	info = PageInfo{Limit: request.Limit, Offset: request.Offset, HasMore: fetched > request.Limit}
	if info.HasMore {
		next := strconv.Itoa(request.Offset + request.Limit)
		info.NextCursor = &next
	}
	if request.Offset > 0 {
		previousOffset := request.Offset - request.Limit
		if previousOffset < 0 {
			previousOffset = 0
		}
		previous := strconv.Itoa(previousOffset)
		info.PreviousCursor = &previous
	}
	return visible, info
}

type UserPage struct {
	Users []User   `json:"users"`
	Page  PageInfo `json:"page"`
}

type APIKeyPage struct {
	Credentials []APIKey `json:"credentials"`
	Page        PageInfo `json:"page"`
}

type SignupInvitationPage struct {
	Invitations []SignupInvitation `json:"invitations"`
	Page        PageInfo           `json:"page"`
}

type TeamPage struct {
	Teams []TeamWithMembers `json:"teams"`
	Page  PageInfo          `json:"page"`
}

type TeamMemberPage struct {
	Members []User   `json:"members"`
	Page    PageInfo `json:"page"`
}

type UserTeamPage struct {
	Teams []Team   `json:"teams"`
	Page  PageInfo `json:"page"`
}

type OwnerContentThreadPage struct {
	Threads []OwnerContentThreadSummary `json:"threads"`
	Page    PageInfo                    `json:"page"`
}
