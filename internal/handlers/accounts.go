package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"decotrack-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type AddLedgerEntryRequest struct {
	Purpose   string    `json:"purpose" binding:"required"`
	Type      string    `json:"type" binding:"required,oneof=openingBalance credit debit"`
	Status    string    `json:"status" binding:"required,oneof=completed pending"`
	Amount    float64   `json:"amount" binding:"required,gt=0"`
	EntryDate time.Time `json:"entryDate" binding:"required"`
}

// FetchClubAccounts lists ledger financial entries, filterable by status.
func (app *App) FetchClubAccounts(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	statusFilter := c.Query("status") // "completed" or "pending"

	// Verify membership
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	var rows pgx.Rows
	queryBase := `
		SELECT id, club_id, purpose, type, status, amount, entry_date, created_at, created_by, updated_at, updated_by
		FROM club_account_entries
		WHERE club_id = $1`

	if statusFilter != "" {
		rows, err = app.DB.Query(c.Request.Context(), queryBase+` AND status = $2 ORDER BY entry_date DESC`, clubID, statusFilter)
	} else {
		rows, err = app.DB.Query(c.Request.Context(), queryBase+` ORDER BY entry_date DESC`, clubID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts: " + err.Error()})
		return
	}
	defer rows.Close()

	entries := []models.ClubAccountEntry{}
	for rows.Next() {
		var entry models.ClubAccountEntry
		var updAt sql.NullTime
		var updBy sql.NullString

		err := rows.Scan(
			&entry.ID, &entry.ClubID, &entry.Purpose, &entry.Type, &entry.Status, &entry.Amount, &entry.EntryDate,
			&entry.CreatedAt, &entry.CreatedBy, &updAt, &updBy,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan ledger entry"})
			return
		}

		if updAt.Valid {
			entry.UpdatedAt = &updAt.Time
		}
		if updBy.Valid {
			entry.UpdatedBy = &updBy.String
		}

		entries = append(entries, entry)
	}

	c.JSON(http.StatusOK, entries)
}

// AddLedgerEntry creates a new credit or debit transaction on the ledger.
func (app *App) AddLedgerEntry(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")

	var req AddLedgerEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify permissions: Owner, Admin, or Secretary
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if status != "active" || (role != "owner" && role != "admin" && role != "secretary") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized. Owner, Admin, or Secretary permissions required."})
		return
	}

	var entry models.ClubAccountEntry
	query := `
		INSERT INTO club_account_entries (club_id, purpose, type, status, amount, entry_date, created_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, club_id, purpose, type, status, amount, entry_date, created_at, created_by`

	err = app.DB.QueryRow(c.Request.Context(), query,
		clubID, req.Purpose, req.Type, req.Status, req.Amount, req.EntryDate, time.Now(), userUID,
	).Scan(&entry.ID, &entry.ClubID, &entry.Purpose, &entry.Type, &entry.Status, &entry.Amount, &entry.EntryDate, &entry.CreatedAt, &entry.CreatedBy)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create ledger entry: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, entry)
}

type UpdateLedgerEntryRequest struct {
	Purpose   string    `json:"purpose" binding:"required"`
	Type      string    `json:"type" binding:"required,oneof=openingBalance credit debit"`
	Status    string    `json:"status" binding:"required,oneof=completed pending"`
	Amount    float64   `json:"amount" binding:"required,gt=0"`
	EntryDate time.Time `json:"entryDate" binding:"required"`
}

// UpdateLedgerEntry updates the ledger transaction details. Requires Owner or Admin role.
func (app *App) UpdateLedgerEntry(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	entryID := c.Param("entryId")

	var req UpdateLedgerEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify permissions: Owner or Admin
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if status != "active" || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized. Owner or Admin permissions required."})
		return
	}

	// Ensure entry exists and belongs to club
	var checkID string
	err = app.DB.QueryRow(c.Request.Context(), `SELECT id FROM club_account_entries WHERE id = $1 AND club_id = $2`, entryID, clubID).Scan(&checkID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Ledger entry not found in this club"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Verification failed: " + err.Error()})
		return
	}

	var entry models.ClubAccountEntry
	var updAt sql.NullTime
	var updBy sql.NullString

	query := `
		UPDATE club_account_entries 
		SET purpose = $1, type = $2, status = $3, amount = $4, entry_date = $5, updated_at = $6, updated_by = $7
		WHERE id = $8 AND club_id = $9
		RETURNING id, club_id, purpose, type, status, amount, entry_date, created_at, created_by, updated_at, updated_by`

	err = app.DB.QueryRow(c.Request.Context(), query,
		req.Purpose, req.Type, req.Status, req.Amount, req.EntryDate, time.Now(), userUID, entryID, clubID,
	).Scan(
		&entry.ID, &entry.ClubID, &entry.Purpose, &entry.Type, &entry.Status, &entry.Amount, &entry.EntryDate,
		&entry.CreatedAt, &entry.CreatedBy, &updAt, &updBy,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update ledger entry: " + err.Error()})
		return
	}

	if updAt.Valid {
		entry.UpdatedAt = &updAt.Time
	}
	if updBy.Valid {
		entry.UpdatedBy = &updBy.String
	}

	c.JSON(http.StatusOK, entry)
}

type PatchStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=completed pending"`
}

// ApproveLedgerEntry transitions transaction status (e.g. pending to completed).
func (app *App) ApproveLedgerEntry(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	entryID := c.Param("entryId")

	var req PatchStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify permissions: Owner, Admin, or Secretary
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if status != "active" || (role != "owner" && role != "admin" && role != "secretary") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized. Owner, Admin, or Secretary permissions required."})
		return
	}

	// Verify entry
	var checkID string
	err = app.DB.QueryRow(c.Request.Context(), `SELECT id FROM club_account_entries WHERE id = $1 AND club_id = $2`, entryID, clubID).Scan(&checkID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Ledger entry not found in this club"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Verification failed: " + err.Error()})
		return
	}

	query := `
		UPDATE club_account_entries 
		SET status = $1, updated_at = $2, updated_by = $3 
		WHERE id = $4 AND club_id = $5`

	_, err = app.DB.Exec(c.Request.Context(), query, req.Status, time.Now(), userUID, entryID, clubID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update status: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":      entryID,
		"status":  req.Status,
		"message": "Transaction status updated successfully and included in active balances.",
	})
}

// FetchRunningBalance computes credit, debit, pending totals, and running net balances.
func (app *App) FetchRunningBalance(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")

	// Verify membership
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	query := `
		SELECT 
			COALESCE(SUM(CASE WHEN type = 'openingBalance' AND status = 'completed' THEN amount ELSE 0 END), 0) as completed_opening,
			COALESCE(SUM(CASE WHEN type = 'credit' AND status = 'completed' THEN amount ELSE 0 END), 0) as completed_credit,
			COALESCE(SUM(CASE WHEN type = 'debit' AND status = 'completed' THEN amount ELSE 0 END), 0) as completed_debit,
			COALESCE(SUM(CASE WHEN type = 'credit' AND status = 'pending' THEN amount ELSE 0 END), 0) as pending_credit,
			COALESCE(SUM(CASE WHEN type = 'debit' AND status = 'pending' THEN amount ELSE 0 END), 0) as pending_debit
		FROM club_account_entries
		WHERE club_id = $1`

	var compOpening, compCredit, compDebit, pendCredit, pendDebit float64
	err = app.DB.QueryRow(c.Request.Context(), query, clubID).Scan(
		&compOpening, &compCredit, &compDebit, &pendCredit, &pendDebit,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute financial aggregates: " + err.Error()})
		return
	}

	// Net balance calculation: completed opening balance + completed credit - completed debit
	runningBalance := (compOpening + compCredit) - compDebit

	c.JSON(http.StatusOK, gin.H{
		"runningBalance":     runningBalance,
		"totalCredit":        compCredit + compOpening, // treated as total positive completed entries
		"totalDebit":         compDebit,
		"totalPendingCredit": pendCredit,
		"totalPendingDebit":  pendDebit,
	})
}
