package handlers

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"decotrack-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type CreateClubRequest struct {
	Name               string `json:"name" binding:"required"`
	Address            string `json:"address" binding:"required"`
	RegistrationNumber string `json:"registrationNumber"`
	Email              string `json:"email" binding:"required,email"`
	PhotoURL           string `json:"photoUrl"`
}

// generateInviteCode creates a random unique alphanumeric code of specified length.
func generateInviteCode(length int) (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	code := make([]byte, length)
	for i := range code {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		code[i] = charset[num.Int64()]
	}
	return string(code), nil
}

// checkRequesterRole checks if the user has a specific role in a club.
func (app *App) getRequesterRoleAndStatus(ctx *gin.Context, clubID, userID string) (string, string, error) {
	var role, status string
	query := `SELECT role, status FROM club_members WHERE club_id = $1 AND user_id = $2`
	err := app.DB.QueryRow(ctx.Request.Context(), query, clubID, userID).Scan(&role, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil // No membership exists
		}
		return "", "", err
	}
	return role, status, nil
}

// CreateClub registers a new club and assigns the creator as the Owner.
func (app *App) CreateClub(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	var req CreateClubRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Begin SQL transaction
	tx, err := app.DB.Begin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(c.Request.Context())

	// Generate unique invite code
	var inviteCode string
	for {
		code, err := generateInviteCode(7)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate invite code"})
			return
		}
		
		var count int
		err = tx.QueryRow(c.Request.Context(), `SELECT COUNT(*) FROM clubs WHERE invite_code = $1`, code).Scan(&count)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify invite code uniqueness"})
			return
		}
		if count == 0 {
			inviteCode = code
			break
		}
	}

	// Insert new Club
	var club models.Club
	insertClubQuery := `
		INSERT INTO clubs (name, address, registration_number, email, photo_url, invite_code, owner_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, name, address, registration_number, email, photo_url, invite_code, owner_id, created_at`

	err = tx.QueryRow(c.Request.Context(), insertClubQuery,
		req.Name, req.Address, req.RegistrationNumber, req.Email, req.PhotoURL, inviteCode, userUID, time.Now(),
	).Scan(&club.ID, &club.Name, &club.Address, &club.RegistrationNumber, &club.Email, &club.PhotoURL, &club.InviteCode, &club.OwnerID, &club.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register club: " + err.Error()})
		return
	}

	// Insert owner member role record
	insertMemberQuery := `
		INSERT INTO club_members (club_id, user_id, role, status, joined_at)
		VALUES ($1, $2, 'owner', 'active', $3)`

	_, err = tx.Exec(c.Request.Context(), insertMemberQuery, club.ID, userUID, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set club owner: " + err.Error()})
		return
	}

	// Commit Transaction
	if err := tx.Commit(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to finalize database changes"})
		return
	}

	c.JSON(http.StatusCreated, club)
}

// ListUserClubs retrieves all clubs where the user is registered.
func (app *App) ListUserClubs(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	query := `
		SELECT c.id, c.name, c.address, c.registration_number, c.email, c.photo_url, c.invite_code, c.owner_id, c.created_at, m.role, m.status
		FROM clubs c
		JOIN club_members m ON c.id = m.club_id
		WHERE m.user_id = $1`

	rows, err := app.DB.Query(c.Request.Context(), query, userUID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query user clubs: " + err.Error()})
		return
	}
	defer rows.Close()

	clubs := []models.Club{}
	for rows.Next() {
		var club models.Club
		err := rows.Scan(
			&club.ID, &club.Name, &club.Address, &club.RegistrationNumber, &club.Email, &club.PhotoURL, &club.InviteCode, &club.OwnerID, &club.CreatedAt,
			&club.MyRole, &club.MyStatus,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read row data"})
			return
		}
		clubs = append(clubs, club)
	}

	c.JSON(http.StatusOK, clubs)
}

// VerifyInviteCode checks code and returns metadata.
func (app *App) VerifyInviteCode(c *gin.Context) {
	code := strings.ToUpper(c.Param("code"))

	query := `
		SELECT c.id, c.name, c.photo_url, u.name
		FROM clubs c
		JOIN users u ON c.owner_id = u.id
		WHERE c.invite_code = $1`

	var clubID, clubName, ownerName string
	var photoURL sql.NullString

	err := app.DB.QueryRow(c.Request.Context(), query, code).Scan(&clubID, &clubName, &photoURL, &ownerName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Invalid invite code"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":        clubID,
		"name":      clubName,
		"photoUrl":  photoURL.String,
		"ownerName": ownerName,
	})
}

type JoinClubRequest struct {
	InviteCode string `json:"inviteCode" binding:"required"`
}

// RequestToJoinClub submits request to join a club (sets role as member, status as pending).
func (app *App) RequestToJoinClub(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	var req JoinClubRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	code := strings.ToUpper(req.InviteCode)

	// Retrieve Club details
	var clubID, clubName string
	err := app.DB.QueryRow(c.Request.Context(), `SELECT id, name FROM clubs WHERE invite_code = $1`, code).Scan(&clubID, &clubName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Club invite code not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database query error: " + err.Error()})
		return
	}

	// Verify if already registered
	var existingStatus string
	err = app.DB.QueryRow(c.Request.Context(), `SELECT status FROM club_members WHERE club_id = $1 AND user_id = $2`, clubID, userUID).Scan(&existingStatus)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{
			"error": fmt.Sprintf("You are already registered in this club. Status: %s", existingStatus),
		})
		return
	}

	// Register user with pending status
	insertQuery := `
		INSERT INTO club_members (club_id, user_id, role, status, joined_at)
		VALUES ($1, $2, 'member', 'pending', $3)`

	_, err = app.DB.Exec(c.Request.Context(), insertQuery, clubID, userUID, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit request to join: " + err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":  "pending_approval",
		"message": fmt.Sprintf("Your request to join %s has been submitted to administrators.", clubName),
	})
}

// ListClubMembers queries members list, optionally filter by status.
func (app *App) ListClubMembers(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	statusFilter := c.Query("status")

	// Validate requester is a member of the club
	reqRole, reqStatus, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify membership: " + err.Error()})
		return
	}
	if reqRole == "" || reqStatus != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Build query
	var rows pgx.Rows
	queryBase := `
		SELECT m.user_id, u.name, u.email, u.mobile_number, u.photo_url, m.role, m.status, m.joined_at
		FROM club_members m
		JOIN users u ON m.user_id = u.id
		WHERE m.club_id = $1`

	if statusFilter != "" {
		rows, err = app.DB.Query(c.Request.Context(), queryBase+` AND m.status = $2`, clubID, statusFilter)
	} else {
		rows, err = app.DB.Query(c.Request.Context(), queryBase, clubID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query members: " + err.Error()})
		return
	}
	defer rows.Close()

	members := []gin.H{}
	for rows.Next() {
		var member struct {
			UserID       string
			Name         string
			Email        string
			MobileNumber sql.NullString
			PhotoURL     sql.NullString
			Role         string
			Status       string
			JoinedAt     time.Time
		}

		err := rows.Scan(
			&member.UserID, &member.Name, &member.Email, &member.MobileNumber, &member.PhotoURL, &member.Role, &member.Status, &member.JoinedAt,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan member records"})
			return
		}

		members = append(members, gin.H{
			"userId":       member.UserID,
			"name":         member.Name,
			"email":        member.Email,
			"mobileNumber": member.MobileNumber.String,
			"photoUrl":     member.PhotoURL.String,
			"role":         member.Role,
			"status":       member.Status,
			"joinedAt":     member.JoinedAt,
		})
	}

	c.JSON(http.StatusOK, members)
}

type UpdateRoleRequest struct {
	Role string `json:"role" binding:"required"`
}

// UpdateMemberRole promotions or demotions.
func (app *App) UpdateMemberRole(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	targetUserID := c.Param("userId")

	var req UpdateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Requester checks: must be owner or admin
	reqRole, reqStatus, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if reqStatus != "active" || (reqRole != "owner" && reqRole != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized. Owner or Admin permissions required."})
		return
	}

	// Prevent updating own role
	if targetUserID == userUID.(string) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot change your own role."})
		return
	}

	// Check if target exists
	var targetRole string
	err = app.DB.QueryRow(c.Request.Context(), `SELECT role FROM club_members WHERE club_id = $1 AND user_id = $2`, clubID, targetUserID).Scan(&targetRole)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Target user is not registered in this club"})
		return
	}

	// Owner check: only owners can demote or promote other admins/owners
	if reqRole == "admin" && (targetRole == "owner" || targetRole == "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Admins cannot modify roles of other Admins or Owners."})
		return
	}

	// Perform update
	query := `UPDATE club_members SET role = $1 WHERE club_id = $2 AND user_id = $3`
	_, err = app.DB.Exec(c.Request.Context(), query, req.Role, clubID, targetUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update member role: " + err.Error()})
		return
	}

	// Log audit action
	var userName string
	app.DB.QueryRow(c.Request.Context(), `SELECT name FROM users WHERE id = $1`, userUID.(string)).Scan(&userName)
	if userName == "" {
		userName = userUID.(string)
	}
	var targetName string
	app.DB.QueryRow(c.Request.Context(), `SELECT name FROM users WHERE id = $1`, targetUserID).Scan(&targetName)
	if targetName == "" {
		targetName = targetUserID
	}
	app.LogActionHelper(c.Request.Context(), clubID, "role_change", userName, fmt.Sprintf("Member: %s", targetName), targetRole, req.Role)

	c.JSON(http.StatusOK, gin.H{
		"userId":  targetUserID,
		"role":    req.Role,
		"message": "Member role updated successfully",
	})
}

type UpdateStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// UpdateMemberStatus approves or blocks memberships.
func (app *App) UpdateMemberStatus(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	targetUserID := c.Param("userId")

	var req UpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Requester checks: must be owner or admin
	reqRole, reqStatus, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if reqStatus != "active" || (reqRole != "owner" && reqRole != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized. Owner or Admin permissions required."})
		return
	}

	// Prevent self modifications
	if targetUserID == userUID.(string) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot change your own membership status."})
		return
	}

	// Check if target exists
	var targetRole string
	err = app.DB.QueryRow(c.Request.Context(), `SELECT role FROM club_members WHERE club_id = $1 AND user_id = $2`, clubID, targetUserID).Scan(&targetRole)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Target user is not registered in this club"})
		return
	}

	// Owner checks: admin cannot modify status of owner or other admins
	if reqRole == "admin" && (targetRole == "owner" || targetRole == "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Admins cannot modify status of other Admins or Owners."})
		return
	}

	// Perform update
	query := `UPDATE club_members SET status = $1 WHERE club_id = $2 AND user_id = $3`
	_, err = app.DB.Exec(c.Request.Context(), query, req.Status, clubID, targetUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update member status: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"userId":  targetUserID,
		"status":  req.Status,
		"message": "Member status updated successfully",
	})
}

// RemoveClubMember removes a member (or rejects a pending request).
func (app *App) RemoveClubMember(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	targetUserID := c.Param("userId")

	// Requester checks: must be owner or admin
	reqRole, reqStatus, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if reqStatus != "active" || (reqRole != "owner" && reqRole != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized. Owner or Admin permissions required."})
		return
	}

	// Prevent self modifications
	if targetUserID == userUID.(string) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "You cannot remove yourself from the club."})
		return
	}

	// Check if target exists
	var targetRole string
	err = app.DB.QueryRow(c.Request.Context(), `SELECT role FROM club_members WHERE club_id = $1 AND user_id = $2`, clubID, targetUserID).Scan(&targetRole)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Target user is not registered in this club"})
		return
	}

	// Owner checks: admin cannot modify role/status of owner or other admins
	if reqRole == "admin" && (targetRole == "owner" || targetRole == "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Admins cannot remove other Admins or Owners."})
		return
	}

	// Perform deletion
	query := `DELETE FROM club_members WHERE club_id = $1 AND user_id = $2`
	_, err = app.DB.Exec(c.Request.Context(), query, clubID, targetUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove member: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"userId":  targetUserID,
		"message": "Member removed successfully",
	})
}
