package models

import (
	"time"
)

// User represents user profile row in users table.
type User struct {
	ID           string    `json:"uid"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	PhotoURL     string    `json:"photoUrl"`
	MobileNumber *string   `json:"mobileNumber"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Club represents clubs table row.
type Club struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Address            string    `json:"address"`
	RegistrationNumber string    `json:"registrationNumber"`
	Email              string    `json:"email"`
	PhotoURL           string    `json:"photoUrl"`
	InviteCode         string    `json:"inviteCode"`
	OwnerID            string    `json:"ownerId"`
	CreatedAt          time.Time `json:"createdAt"`
	MyRole             string    `json:"myRole,omitempty"`
	MyStatus           string    `json:"myStatus,omitempty"`
}

// ClubMember represents the club_members pivot table.
type ClubMember struct {
	ClubID   string    `json:"clubId"`
	UserID   string    `json:"userId"`
	Role     string    `json:"role"`
	Status   string    `json:"status"`
	JoinedAt time.Time `json:"joinedAt"`
}

// UserFCMToken represents device FCM tokens linked to a user.
type UserFCMToken struct {
	UserID      string    `json:"userId"`
	Token       string    `json:"token"`
	Platform    string    `json:"platform"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// Inventory represents inventory table row.
type Inventory struct {
	ID           string    `json:"id"`
	ClubID       string    `json:"clubId"`
	Name         string    `json:"name"`
	TotalQty     int       `json:"totalQty"`
	AvailableQty int       `json:"availableQty"`
	Category     string    `json:"category"`
	IsActive     bool      `json:"isActive"`
	Type         string    `json:"type"` // "lent" or "catering"
	CreatedAt    time.Time `json:"createdAt"`
}

// Lending represents an active or closed lending order.
type Lending struct {
	ID                 string     `json:"id"`
	ClubID             string     `json:"clubId"`
	CustomerName       string     `json:"customerName"`
	CustomerMobile     *string    `json:"customerMobile"`
	CustomerAddress    *string    `json:"customerAddress"`
	Purpose            *string    `json:"purpose"`
	ExpectedReturnDate *time.Time `json:"expectedReturnDate"`
	Amount             float64    `json:"amount"`
	Status             string     `json:"status"` // "active", "partiallyReturned", "closed"
	CreatedAt          time.Time  `json:"createdAt"`
	CreatedBy          string     `json:"createdBy"`
}

// LendingItem represents an item within a lending order.
type LendingItem struct {
	LendingID        string `json:"lendingId"`
	InventoryID      string `json:"itemId"`
	Quantity         int    `json:"quantity"`
	ReturnedQuantity int    `json:"returnedQuantity"`
	Name             string `json:"name,omitempty"` // Derived from join
}

// LendingReturn represents a return log entry.
type LendingReturn struct {
	ID        string    `json:"id"`
	LendingID string    `json:"lendingId"`
	Note      *string   `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy"`
}

// LendingReturnItem represents an item in a return transaction.
type LendingReturnItem struct {
	ReturnID    string `json:"returnId"`
	InventoryID string `json:"itemId"`
	Quantity    int    `json:"quantity"`
}

// Notice represents notice board announcement, event, or poll.
type Notice struct {
	ID        string                 `json:"id"`
	ClubID    string                 `json:"clubId"`
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	IsEvent   bool                   `json:"isEvent"`
	CreatedAt time.Time              `json:"createdAt"`
	CreatedBy string                 `json:"createdBy"`
	Options   []string               `json:"options,omitempty"` // Input/Output helper for poll options
	Votes     map[string]int         `json:"votes,omitempty"`   // OptionText -> VoteCount
	MyVote    *string                `json:"myVote,omitempty"`  // Requester's casted option text, if any
}

// NoticeOption represents an option in a poll.
type NoticeOption struct {
	ID       string `json:"id"`
	NoticeID string `json:"noticeId"`
	Option   string `json:"optionText"`
}

// NoticeVote represents a single casted vote.
type NoticeVote struct {
	NoticeID string `json:"noticeId"`
	UserID   string `json:"userId"`
	OptionID string `json:"optionId"`
}

// ClubAccountEntry represents club_account_entries ledger.
type ClubAccountEntry struct {
	ID        string     `json:"id"`
	ClubID    string     `json:"clubId"`
	Purpose   string     `json:"purpose"`
	Type      string     `json:"type"` // "openingBalance", "credit", "debit"
	Status    string     `json:"status"` // "completed", "pending"
	Amount    float64    `json:"amount"`
	EntryDate time.Time  `json:"entryDate"`
	CreatedAt time.Time  `json:"createdAt"`
	CreatedBy string     `json:"createdBy"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy *string    `json:"updatedBy,omitempty"`
}

// AuditLog represents a record in audit_logs.
type AuditLog struct {
	ID           string    `json:"id"`
	ClubID       string    `json:"clubId"`
	ActionType   string    `json:"actionType"`
	PerformedBy  string    `json:"performedBy"`
	TargetEntity string    `json:"targetEntity"`
	OldValue     any       `json:"oldValue"`
	NewValue     any       `json:"newValue"`
	Timestamp    time.Time `json:"timestamp"`
}

// Notification represents a record in notifications table.
type Notification struct {
	ID        string    `json:"id"`
	ClubID    string    `json:"clubId"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	IsRead    bool      `json:"isRead"`
	CreatedAt time.Time `json:"createdAt"`
}
