package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type LendingItemReq struct {
	ItemID   string `json:"itemId" binding:"required"`
	Quantity int    `json:"quantity" binding:"required,min=1"`
}

type CreateLendingRequest struct {
	CustomerName       string           `json:"customerName" binding:"required"`
	CustomerMobile     string           `json:"customerMobile"`
	CustomerAddress    string           `json:"customerAddress"`
	Purpose            string           `json:"purpose"`
	ExpectedReturnDate *time.Time       `json:"expectedReturnDate"`
	Amount             float64          `json:"amount" binding:"min=0"`
	Items              []LendingItemReq `json:"items" binding:"required,min=1"`
}

// CreateLending performs ACID stock checkout and returns contract state.
func (app *App) CreateLending(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")

	var req CreateLendingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify requester membership
	role, status, err := app.getRequesterRoleAndStatus(*c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Begin ACID Database Transaction
	tx, err := app.DB.Begin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open database transaction"})
		return
	}
	defer tx.Rollback(c.Request.Context())

	// 1. Lock and Verify Stock levels for all items
	for _, item := range req.Items {
		var availableQty int
		var itemName string
		var isItemActive bool

		err = tx.QueryRow(c.Request.Context(), `
			SELECT name, available_qty, is_active 
			FROM inventory 
			WHERE id = $1 AND club_id = $2 
			FOR UPDATE`, item.ItemID, clubID).Scan(&itemName, &availableQty, &isItemActive)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Asset item %s not found in this club", item.ItemID)})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database stock verification error: " + err.Error()})
			return
		}

		if !isItemActive {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Asset '%s' is currently marked inactive and cannot be checked out", itemName)})
			return
		}

		if availableQty < item.Quantity {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Insufficient stock for: %s (Requested: %d, Available: %d)", itemName, item.Quantity, availableQty),
			})
			return
		}
	}

	// 2. Insert main Lending Contract
	var lendingID string
	insertLendingQuery := `
		INSERT INTO lendings (club_id, customer_name, customer_mobile, customer_address, purpose, expected_return_date, amount, status, created_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8, $9)
		RETURNING id`

	err = tx.QueryRow(c.Request.Context(), insertLendingQuery,
		clubID, req.CustomerName, req.CustomerMobile, req.CustomerAddress, req.Purpose, req.ExpectedReturnDate, req.Amount, time.Now(), userUID,
	).Scan(&lendingID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create lending record: " + err.Error()})
		return
	}

	// 3. Write Lending Items & Decrement Inventory quantities
	for _, item := range req.Items {
		// Insert checkout log item
		_, err = tx.Exec(c.Request.Context(), `
			INSERT INTO lending_items (lending_id, inventory_id, quantity, returned_quantity)
			VALUES ($1, $2, $3, 0)`, lendingID, item.ItemID, item.Quantity)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register lending item details: " + err.Error()})
			return
		}

		// Decrement available stock
		_, err = tx.Exec(c.Request.Context(), `
			UPDATE inventory 
			SET available_qty = available_qty - $1 
			WHERE id = $2 AND club_id = $3`, item.Quantity, item.ItemID, clubID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update item inventory: " + err.Error()})
			return
		}
	}

	// Commit Transaction
	if err := tx.Commit(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"lendingId": lendingID,
		"status":    "active",
		"message":   "Lending record created and inventory quantities successfully updated.",
	})
}

type ReturnItemReq struct {
	ItemID   string `json:"itemId" binding:"required"`
	Quantity int    `json:"quantity" binding:"required,min=1"`
}

type RecordReturnRequest struct {
	Items []ReturnItemReq `json:"items" binding:"required,min=1"`
	Note  string          `json:"note"`
}

// RecordReturn restores stock levels and logs full or partial item check-ins.
func (app *App) RecordReturn(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	lendingID := c.Param("lendingId")

	var req RecordReturnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify requester membership
	role, status, err := app.getRequesterRoleAndStatus(*c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Begin ACID Database Transaction
	tx, err := app.DB.Begin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open database transaction"})
		return
	}
	defer tx.Rollback(c.Request.Context())

	// 1. Lock and Verify Lending Contract
	var currentStatus string
	err = tx.QueryRow(c.Request.Context(), `
		SELECT status FROM lendings 
		WHERE id = $1 AND club_id = $2 
		FOR UPDATE`, lendingID, clubID).Scan(&currentStatus)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Lending record not found in this club"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Lending verification failed: " + err.Error()})
		return
	}

	if currentStatus == "closed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "This lending record is already closed. All items have been returned."})
		return
	}

	// 2. Create Return Entry
	var returnID string
	insertReturnQuery := `
		INSERT INTO lending_returns (lending_id, note, created_at, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id`

	err = tx.QueryRow(c.Request.Context(), insertReturnQuery,
		lendingID, req.Note, time.Now(), userUID,
	).Scan(&returnID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create return record: " + err.Error()})
		return
	}

	// 3. Process each returned item
	for _, item := range req.Items {
		// Verify item belongs to checkout contract with locking
		var checkoutQty, returnedQty int
		err = tx.QueryRow(c.Request.Context(), `
			SELECT quantity, returned_quantity 
			FROM lending_items 
			WHERE lending_id = $1 AND inventory_id = $2 
			FOR UPDATE`, lendingID, item.ItemID).Scan(&checkoutQty, &returnedQty)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Asset item %s is not part of this lending contract", item.ItemID)})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database item checkout check failed: " + err.Error()})
			return
		}

		// Check if returning more than checked out
		remainingToReturn := checkoutQty - returnedQty
		if item.Quantity > remainingToReturn {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Over-return error: Requested to return %d, but only %d remains outstanding", item.Quantity, remainingToReturn),
			})
			return
		}

		// Log return item
		_, err = tx.Exec(c.Request.Context(), `
			INSERT INTO lending_return_items (return_id, inventory_id, quantity)
			VALUES ($1, $2, $3)`, returnID, item.ItemID, item.Quantity)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to log return items: " + err.Error()})
			return
		}

		// Increment returned quantity in contract record
		_, err = tx.Exec(c.Request.Context(), `
			UPDATE lending_items 
			SET returned_quantity = returned_quantity + $1 
			WHERE lending_id = $2 AND inventory_id = $3`, item.Quantity, lendingID, item.ItemID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update return counts: " + err.Error()})
			return
		}

		// Restore stock levels
		_, err = tx.Exec(c.Request.Context(), `
			UPDATE inventory 
			SET available_qty = available_qty + $1 
			WHERE id = $2 AND club_id = $3`, item.Quantity, item.ItemID, clubID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore asset stock levels: " + err.Error()})
			return
		}
	}

	// 4. Update Lending Contract Status dynamically
	// Check if all items in this contract have been fully returned
	var totalItems, fullyReturnedItems int
	err = tx.QueryRow(c.Request.Context(), `
		SELECT COUNT(*), SUM(CASE WHEN quantity = returned_quantity THEN 1 ELSE 0 END) 
		FROM lending_items 
		WHERE lending_id = $1`, lendingID).Scan(&totalItems, &fullyReturnedItems)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute final lending status: " + err.Error()})
		return
	}

	var newLendingStatus string
	if totalItems == fullyReturnedItems {
		newLendingStatus = "closed"
	} else {
		// Check if at least some items are returned
		var totalReturnedSum int
		err = tx.QueryRow(c.Request.Context(), `SELECT COALESCE(SUM(returned_quantity), 0) FROM lending_items WHERE lending_id = $1`, lendingID).Scan(&totalReturnedSum)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check partial status"})
			return
		}
		if totalReturnedSum > 0 {
			newLendingStatus = "partiallyReturned"
		} else {
			newLendingStatus = "active"
		}
	}

	_, err = tx.Exec(c.Request.Context(), `UPDATE lendings SET status = $1 WHERE id = $2`, newLendingStatus, lendingID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update final contract status: " + err.Error()})
		return
	}

	// Commit ACID operations
	if err := tx.Commit(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to finalize return database changes"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"returnId":      returnID,
		"lendingStatus": newLendingStatus,
		"message":       "Return recorded and inventory stocks restored successfully.",
	})
}
