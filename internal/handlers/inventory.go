package handlers

import (
	"errors"
	"net/http"
	"time"

	"decotrack-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type AddAssetRequest struct {
	Name     string `json:"name" binding:"required"`
	TotalQty int    `json:"totalQty" binding:"required,min=0"`
	Category string `json:"category" binding:"required"`
	Type     string `json:"type" binding:"required,oneof=lent catering"`
}

// FetchClubInventory lists assets of the club, optionally filtered by type.
func (app *App) FetchClubInventory(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	invType := c.Query("type") // "lent" or "catering"

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
		SELECT id, club_id, name, total_qty, available_qty, category, is_active, type, created_at
		FROM inventory
		WHERE club_id = $1`

	if invType != "" {
		rows, err = app.DB.Query(c.Request.Context(), queryBase+` AND type = $2`, clubID, invType)
	} else {
		rows, err = app.DB.Query(c.Request.Context(), queryBase, clubID)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch inventory: " + err.Error()})
		return
	}
	defer rows.Close()

	inventoryList := []models.Inventory{}
	for rows.Next() {
		var item models.Inventory
		err := rows.Scan(
			&item.ID, &item.ClubID, &item.Name, &item.TotalQty, &item.AvailableQty, &item.Category, &item.IsActive, &item.Type, &item.CreatedAt,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read inventory record"})
			return
		}
		inventoryList = append(inventoryList, item)
	}

	c.JSON(http.StatusOK, inventoryList)
}

// AddInventoryAsset registers a new catalog asset (sets available_qty = total_qty).
func (app *App) AddInventoryAsset(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")

	var req AddAssetRequest
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

	var item models.Inventory
	query := `
		INSERT INTO inventory (club_id, name, total_qty, available_qty, category, type, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, club_id, name, total_qty, available_qty, category, is_active, type, created_at`

	err = app.DB.QueryRow(c.Request.Context(), query,
		clubID, req.Name, req.TotalQty, req.TotalQty, req.Category, req.Type, time.Now(),
	).Scan(&item.ID, &item.ClubID, &item.Name, &item.TotalQty, &item.AvailableQty, &item.Category, &item.IsActive, &item.Type, &item.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register inventory asset: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, item)
}

type UpdateAssetRequest struct {
	Name     string `json:"name" binding:"required"`
	TotalQty int    `json:"totalQty" binding:"required,min=0"`
	Category string `json:"category" binding:"required"`
	IsActive bool   `json:"isActive"`
}

// UpdateInventoryAsset modifies item parameters, adjusting available quantities dynamically.
func (app *App) UpdateInventoryAsset(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	itemID := c.Param("itemId")

	var req UpdateAssetRequest
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

	// Begin Transaction to perform safe quantity shift calculations
	tx, err := app.DB.Begin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(c.Request.Context())

	// Fetch current item details with row lock
	var oldTotal, oldAvailable int
	err = tx.QueryRow(c.Request.Context(), `
		SELECT total_qty, available_qty 
		FROM inventory 
		WHERE id = $1 AND club_id = $2 
		FOR UPDATE`, itemID, clubID).Scan(&oldTotal, &oldAvailable)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Asset item not found in this club"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query asset: " + err.Error()})
		return
	}

	// Calculate new available quantity
	difference := req.TotalQty - oldTotal
	newAvailable := oldAvailable + difference

	if newAvailable < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Total quantity reduction is impossible because too many items are currently checked out/lent.",
		})
		return
	}

	var item models.Inventory
	updateQuery := `
		UPDATE inventory 
		SET name = $1, total_qty = $2, available_qty = $3, category = $4, is_active = $5 
		WHERE id = $6 AND club_id = $7
		RETURNING id, club_id, name, total_qty, available_qty, category, is_active, type, created_at`

	err = tx.QueryRow(c.Request.Context(), updateQuery,
		req.Name, req.TotalQty, newAvailable, req.Category, req.IsActive, itemID, clubID,
	).Scan(&item.ID, &item.ClubID, &item.Name, &item.TotalQty, &item.AvailableQty, &item.Category, &item.IsActive, &item.Type, &item.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update asset in database: " + err.Error()})
		return
	}

	if err := tx.Commit(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit database updates"})
		return
	}

	c.JSON(http.StatusOK, item)
}

// DeleteInventoryAsset removes an asset from the inventory.
func (app *App) DeleteInventoryAsset(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	itemID := c.Param("itemId")

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

	// Delete item from database
	query := `DELETE FROM inventory WHERE id = $1 AND club_id = $2`
	res, err := app.DB.Exec(c.Request.Context(), query, itemID, clubID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete asset from database: " + err.Error()})
		return
	}

	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Asset item not found in this club"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":      itemID,
		"message": "Inventory asset deleted successfully",
	})
}
