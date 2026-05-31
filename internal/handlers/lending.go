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
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
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

	// Log audit action
	var userName string
	app.DB.QueryRow(c.Request.Context(), `SELECT name FROM users WHERE id = $1`, userUID.(string)).Scan(&userName)
	if userName == "" {
		userName = userUID.(string)
	}
	app.LogActionHelper(c.Request.Context(), clubID, "lending_created", userName, fmt.Sprintf("Lending: %s", req.CustomerName), nil, req)

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
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
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

	// Log audit action
	var userName string
	app.DB.QueryRow(c.Request.Context(), `SELECT name FROM users WHERE id = $1`, userUID.(string)).Scan(&userName)
	if userName == "" {
		userName = userUID.(string)
	}

	actionType := "return_partial"
	if newLendingStatus == "closed" {
		actionType = "return_full"
	}
	app.LogActionHelper(c.Request.Context(), clubID, actionType, userName, fmt.Sprintf("Return: %s", lendingID), nil, req)

	c.JSON(http.StatusOK, gin.H{
		"returnId":      returnID,
		"lendingStatus": newLendingStatus,
		"message":       "Return recorded and inventory stocks restored successfully.",
	})
}

type LendingItemResp struct {
	ItemID   string `json:"itemId"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type LendingResp struct {
	ID                 string            `json:"id"`
	ClubID             string            `json:"clubId"`
	CustomerName       string            `json:"customerName"`
	CustomerMobile     string            `json:"customerMobile"`
	CustomerAddress    string            `json:"customerAddress"`
	Purpose            string            `json:"purpose"`
	ExpectedReturnDate *time.Time        `json:"expectedReturnDate"`
	Amount             float64           `json:"amount"`
	Items              []LendingItemResp `json:"items"`
	Status             string            `json:"status"`
	Version            int               `json:"version"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
	LentById           string            `json:"lentById"`
	LentBy             string            `json:"lentBy"`
}

// ListLendings retrieves all lending records for a club, including their items.
func (app *App) ListLendings(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")

	// Verify requester membership
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Fetch all lendings
	query := `
		SELECT l.id, l.club_id, l.customer_name, COALESCE(l.customer_mobile, ''), COALESCE(l.customer_address, ''), 
		       COALESCE(l.purpose, ''), l.expected_return_date, COALESCE(l.amount, 0), l.status, l.created_at, 
		       COALESCE(l.created_by, ''), COALESCE(u.name, 'System')
		FROM lendings l
		LEFT JOIN users u ON l.created_by = u.id
		WHERE l.club_id = $1
		ORDER BY l.created_at DESC`

	rows, err := app.DB.Query(c.Request.Context(), query, clubID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query lendings: " + err.Error()})
		return
	}
	defer rows.Close()

	lendings := []LendingResp{}
	for rows.Next() {
		var l LendingResp
		err := rows.Scan(
			&l.ID, &l.ClubID, &l.CustomerName, &l.CustomerMobile, &l.CustomerAddress,
			&l.Purpose, &l.ExpectedReturnDate, &l.Amount, &l.Status, &l.CreatedAt,
			&l.LentById, &l.LentBy,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read lending record: " + err.Error()})
			return
		}
		l.Version = 1
		l.UpdatedAt = l.CreatedAt // fallback
		lendings = append(lendings, l)
	}

	// For each lending, fetch its associated items
	for i, l := range lendings {
		itemRows, err := app.DB.Query(c.Request.Context(), `
			SELECT li.inventory_id, i.name, li.quantity
			FROM lending_items li
			JOIN inventory i ON li.inventory_id = i.id
			WHERE li.lending_id = $1`, l.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query lending items: " + err.Error()})
			return
		}
		defer itemRows.Close()

		items := []LendingItemResp{}
		for itemRows.Next() {
			var item LendingItemResp
			err := itemRows.Scan(&item.ItemID, &item.Name, &item.Quantity)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read lending item: " + err.Error()})
				return
			}
			items = append(items, item)
		}
		lendings[i].Items = items
	}

	c.JSON(http.StatusOK, lendings)
}

// GetLendingByID retrieves a single lending record by its ID, including its items.
func (app *App) GetLendingByID(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	lendingID := c.Param("lendingId")

	// Verify requester membership
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Fetch the lending
	var l LendingResp
	query := `
		SELECT l.id, l.club_id, l.customer_name, COALESCE(l.customer_mobile, ''), COALESCE(l.customer_address, ''), 
		       COALESCE(l.purpose, ''), l.expected_return_date, COALESCE(l.amount, 0), l.status, l.created_at, 
		       COALESCE(l.created_by, ''), COALESCE(u.name, 'System')
		FROM lendings l
		LEFT JOIN users u ON l.created_by = u.id
		WHERE l.id = $1 AND l.club_id = $2`

	err = app.DB.QueryRow(c.Request.Context(), query, lendingID, clubID).Scan(
		&l.ID, &l.ClubID, &l.CustomerName, &l.CustomerMobile, &l.CustomerAddress,
		&l.Purpose, &l.ExpectedReturnDate, &l.Amount, &l.Status, &l.CreatedAt,
		&l.LentById, &l.LentBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Lending record not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch lending record: " + err.Error()})
		return
	}
	l.Version = 1
	l.UpdatedAt = l.CreatedAt

	// Fetch items
	itemRows, err := app.DB.Query(c.Request.Context(), `
		SELECT li.inventory_id, i.name, li.quantity
		FROM lending_items li
		JOIN inventory i ON li.inventory_id = i.id
		WHERE li.lending_id = $1`, l.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query lending items: " + err.Error()})
		return
	}
	defer itemRows.Close()

	items := []LendingItemResp{}
	for itemRows.Next() {
		var item LendingItemResp
		err := itemRows.Scan(&item.ItemID, &item.Name, &item.Quantity)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read lending item: " + err.Error()})
			return
		}
		items = append(items, item)
	}
	l.Items = items

	c.JSON(http.StatusOK, l)
}

type LendingReturnItemResp struct {
	ItemID   string `json:"itemId"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type LendingReturnResp struct {
	ID          string                  `json:"id"`
	LendingID   string                  `json:"lendingId"`
	ClubID      string                  `json:"clubId"`
	PerformedBy string                  `json:"performedBy"`
	ReturnedAt  time.Time               `json:"returnedAt"`
	Items       []LendingReturnItemResp `json:"items"`
	Note        string                  `json:"note"`
	Version     int                     `json:"version"`
}

// ListLendingReturns lists all returns registered for a specific lending order.
func (app *App) ListLendingReturns(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	lendingID := c.Param("lendingId")

	// Verify requester membership
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Query returns
	query := `
		SELECT r.id, r.lending_id, COALESCE(r.note, ''), r.created_at, COALESCE(u.name, 'System')
		FROM lending_returns r
		LEFT JOIN users u ON r.created_by = u.id
		WHERE r.lending_id = $1
		ORDER BY r.created_at DESC`

	rows, err := app.DB.Query(c.Request.Context(), query, lendingID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch returns: " + err.Error()})
		return
	}
	defer rows.Close()

	returns := []LendingReturnResp{}
	for rows.Next() {
		var r LendingReturnResp
		err := rows.Scan(&r.ID, &r.LendingID, &r.Note, &r.ReturnedAt, &r.PerformedBy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read return record: " + err.Error()})
			return
		}
		r.ClubID = clubID
		r.Version = 1
		returns = append(returns, r)
	}

	// Query return items for each return log
	for i, r := range returns {
		itemRows, err := app.DB.Query(c.Request.Context(), `
			SELECT ri.inventory_id, i.name, ri.quantity
			FROM lending_return_items ri
			JOIN inventory i ON ri.inventory_id = i.id
			WHERE ri.return_id = $1`, r.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch return items: " + err.Error()})
			return
		}
		defer itemRows.Close()

		items := []LendingReturnItemResp{}
		for itemRows.Next() {
			var item LendingReturnItemResp
			err := itemRows.Scan(&item.ItemID, &item.Name, &item.Quantity)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read return item: " + err.Error()})
				return
			}
			items = append(items, item)
		}
		returns[i].Items = items
	}

	c.JSON(http.StatusOK, returns)
}

type UpdateLendingRequest struct {
	CustomerName       string     `json:"customerName" binding:"required"`
	CustomerMobile     string     `json:"customerMobile"`
	CustomerAddress    string     `json:"customerAddress"`
	Purpose            string     `json:"purpose"`
	ExpectedReturnDate *time.Time `json:"expectedReturnDate"`
	Amount             float64    `json:"amount" binding:"min=0"`
}

// UpdateLending updates basic metadata fields of an active lending order.
func (app *App) UpdateLending(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	lendingID := c.Param("lendingId")

	var req UpdateLendingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify requester membership
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Check if record exists
	var count int
	err = app.DB.QueryRow(c.Request.Context(), `SELECT COUNT(1) FROM lendings WHERE id = $1 AND club_id = $2`, lendingID, clubID).Scan(&count)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error: " + err.Error()})
		return
	}
	if count == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Lending record not found"})
		return
	}

	// Update metadata
	query := `
		UPDATE lendings
		SET customer_name = $1, customer_mobile = $2, customer_address = $3, purpose = $4, expected_return_date = $5, amount = $6
		WHERE id = $7 AND club_id = $8`

	_, err = app.DB.Exec(c.Request.Context(), query,
		req.CustomerName, req.CustomerMobile, req.CustomerAddress, req.Purpose, req.ExpectedReturnDate, req.Amount,
		lendingID, clubID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update lending record: " + err.Error()})
		return
	}

	// Log audit action
	var userName string
	app.DB.QueryRow(c.Request.Context(), `SELECT name FROM users WHERE id = $1`, userUID.(string)).Scan(&userName)
	if userName == "" {
		userName = userUID.(string)
	}
	app.LogActionHelper(c.Request.Context(), clubID, "lending_updated", userName, fmt.Sprintf("Lending: %s", req.CustomerName), nil, req)

	// Fetch and return the updated lending
	var l LendingResp
	fetchQuery := `
		SELECT l.id, l.club_id, l.customer_name, COALESCE(l.customer_mobile, ''), COALESCE(l.customer_address, ''), 
		       COALESCE(l.purpose, ''), l.expected_return_date, COALESCE(l.amount, 0), l.status, l.created_at, 
		       COALESCE(l.created_by, ''), COALESCE(u.name, 'System')
		FROM lendings l
		LEFT JOIN users u ON l.created_by = u.id
		WHERE l.id = $1 AND l.club_id = $2`

	err = app.DB.QueryRow(c.Request.Context(), fetchQuery, lendingID, clubID).Scan(
		&l.ID, &l.ClubID, &l.CustomerName, &l.CustomerMobile, &l.CustomerAddress,
		&l.Purpose, &l.ExpectedReturnDate, &l.Amount, &l.Status, &l.CreatedAt,
		&l.LentById, &l.LentBy,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch updated record: " + err.Error()})
		return
	}
	l.Version = 1
	l.UpdatedAt = time.Now()

	// Fetch items
	itemRows, err := app.DB.Query(c.Request.Context(), `
		SELECT li.inventory_id, i.name, li.quantity
		FROM lending_items li
		JOIN inventory i ON li.inventory_id = i.id
		WHERE li.lending_id = $1`, l.ID)
	if err == nil {
		defer itemRows.Close()
		items := []LendingItemResp{}
		for itemRows.Next() {
			var item LendingItemResp
			_ = itemRows.Scan(&item.ItemID, &item.Name, &item.Quantity)
			items = append(items, item)
		}
		l.Items = items
	}

	c.JSON(http.StatusOK, l)
}

