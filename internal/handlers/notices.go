package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"decotrack-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type CreateNoticeRequest struct {
	Title   string   `json:"title" binding:"required"`
	Message string   `json:"message" binding:"required"`
	IsEvent bool     `json:"isEvent"`
	Options []string `json:"options"` // Optional for poll options
}

// CreateNotice registers a new announcement and dispatches background FCM push alerts.
func (app *App) CreateNotice(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")

	var req CreateNoticeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify requester is a member of the club
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Begin SQL Transaction
	tx, err := app.DB.Begin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start database transaction"})
		return
	}
	defer tx.Rollback(c.Request.Context())

	// Insert notice announcement
	var notice models.Notice
	insertNoticeQuery := `
		INSERT INTO notices (club_id, title, message, is_event, created_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, club_id, title, message, is_event, created_at, created_by`

	err = tx.QueryRow(c.Request.Context(), insertNoticeQuery,
		clubID, req.Title, req.Message, req.IsEvent, time.Now(), userUID,
	).Scan(&notice.ID, &notice.ClubID, &notice.Title, &notice.Message, &notice.IsEvent, &notice.CreatedAt, &notice.CreatedBy)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register notice entry: " + err.Error()})
		return
	}

	// Insert poll options if provided
	votesMap := make(map[string]int)
	if len(req.Options) > 0 {
		notice.Options = req.Options
		for _, option := range req.Options {
			_, err = tx.Exec(c.Request.Context(), `
				INSERT INTO notice_options (notice_id, option_text)
				VALUES ($1, $2)`, notice.ID, option)

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert poll options: " + err.Error()})
				return
			}
			votesMap[option] = 0 // Initialize counts to zero
		}
		notice.Votes = votesMap
	}

	// Commit Transaction
	if err := tx.Commit(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to finalize database updates"})
		return
	}

	// 📡 Spawn background worker to query FCM tokens and dispatch alerts asynchronously
	go app.dispatchFCMNotificationBackground(clubID, notice.Title, notice.Message)

	c.JSON(http.StatusCreated, notice)
}

// dispatchFCMNotificationBackground queries tokens and sends push notifications asynchronously.
func (app *App) dispatchFCMNotificationBackground(clubID, title, body string) {
	log.Printf("[FCM BACKGROUND WORKER] Fetching active member device tokens for club %s...", clubID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Query registered FCM tokens of active members
	query := `
		SELECT t.token 
		FROM user_fcm_tokens t
		JOIN club_members m ON t.user_id = m.user_id
		WHERE m.club_id = $1 AND m.status = 'active'`

	rows, err := app.DB.Query(ctx, query, clubID)
	if err != nil {
		log.Printf("[FCM BACKGROUND WORKER ERROR] Failed to query member tokens: %v", err)
		return
	}
	defer rows.Close()

	tokens := []string{}
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err == nil {
			tokens = append(tokens, token)
		}
	}

	if len(tokens) == 0 {
		log.Printf("[FCM BACKGROUND WORKER] No registered device tokens found in club %s. Skipping push.", clubID)
		return
	}

	log.Printf("[FCM BACKGROUND WORKER] Dispatching push notifications to %d device(s)...", len(tokens))
	for _, token := range tokens {
		go func(tok string) {
			subCtx, subCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer subCancel()
			
			if err := app.FCM.SendPushNotification(subCtx, tok, title, body); err != nil {
				log.Printf("[FCM BACKGROUND DISPATCH ERROR] Failed to notify token %s: %v", tok, err)
			}
		}(token)
	}
}

type CastVoteRequest struct {
	Option string `json:"option" binding:"required"`
}

// CastVote registers a user's choice in a notice board voting poll.
func (app *App) CastVote(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	noticeID := c.Param("noticeId")

	var req CastVoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify requester is a member of the club
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Retrieve target Option ID and verify it belongs to this Notice
	var optionID string
	err = app.DB.QueryRow(c.Request.Context(), `
		SELECT id FROM notice_options 
		WHERE notice_id = $1 AND option_text = $2`, noticeID, req.Option).Scan(&optionID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid choice option for this poll"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify poll option: " + err.Error()})
		return
	}

	// Register / Upsert Vote Choice
	voteQuery := `
		INSERT INTO notice_votes (notice_id, user_id, option_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (notice_id, user_id) DO UPDATE 
		SET option_id = EXCLUDED.option_id`

	_, err = app.DB.Exec(c.Request.Context(), voteQuery, noticeID, userUID, optionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register vote choice: " + err.Error()})
		return
	}

	// Fetch updated poll totals
	totalsQuery := `
		SELECT o.option_text, COUNT(v.user_id) 
		FROM notice_options o
		LEFT JOIN notice_votes v ON o.id = v.option_id
		WHERE o.notice_id = $1
		GROUP BY o.option_text`

	rows, err := app.DB.Query(c.Request.Context(), totalsQuery, noticeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch updated vote totals: " + err.Error()})
		return
	}
	defer rows.Close()

	updatedVotes := make(map[string]int)
	for rows.Next() {
		var optText string
		var voteCount int
		if err := rows.Scan(&optText, &voteCount); err == nil {
			updatedVotes[optText] = voteCount
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"noticeId": noticeID,
		"votes":    updatedVotes,
		"message":  "Vote cast successfully",
	})
}

type NoticeResp struct {
	ID        string         `json:"id"`
	ClubID    string         `json:"clubId"`
	Title     string         `json:"title"`
	Message   string         `json:"message"`
	IsEvent   bool           `json:"isEvent"`
	CreatedAt time.Time      `json:"createdAt"`
	CreatedBy string         `json:"createdBy"`
	Options   []string       `json:"options"`
	Votes     map[string]int `json:"votes"`
	MyVote    *string        `json:"myVote,omitempty"`
}

// ListNotices retrieves all announcements, RSVP events, and polls for a club.
func (app *App) ListNotices(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")

	// Verify requester is a member of the club
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Fetch all notices
	query := `
		SELECT n.id, n.club_id, n.title, n.message, n.is_event, n.created_at, COALESCE(u.name, 'System')
		FROM notices n
		LEFT JOIN users u ON n.created_by = u.id
		WHERE n.club_id = $1
		ORDER BY n.created_at DESC`

	rows, err := app.DB.Query(c.Request.Context(), query, clubID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query notices: " + err.Error()})
		return
	}
	defer rows.Close()

	notices := []NoticeResp{}
	for rows.Next() {
		var n NoticeResp
		err := rows.Scan(&n.ID, &n.ClubID, &n.Title, &n.Message, &n.IsEvent, &n.CreatedAt, &n.CreatedBy)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan notice: " + err.Error()})
			return
		}
		n.Options = []string{}
		n.Votes = make(map[string]int)
		notices = append(notices, n)
	}

	// For each notice, fetch its poll options, vote totals, and the current user's vote
	for i, n := range notices {
		// 1. Fetch options
		optRows, err := app.DB.Query(c.Request.Context(), `SELECT option_text FROM notice_options WHERE notice_id = $1`, n.ID)
		if err == nil {
			options := []string{}
			for optRows.Next() {
				var opt string
				if err := optRows.Scan(&opt); err == nil {
					options = append(options, opt)
				}
			}
			optRows.Close()
			notices[i].Options = options
		}

		// 2. Fetch vote totals
		totalsQuery := `
			SELECT o.option_text, COUNT(v.user_id) 
			FROM notice_options o
			LEFT JOIN notice_votes v ON o.id = v.option_id
			WHERE o.notice_id = $1
			GROUP BY o.option_text`
		totRows, err := app.DB.Query(c.Request.Context(), totalsQuery, n.ID)
		if err == nil {
			votesMap := make(map[string]int)
			for totRows.Next() {
				var optText string
				var voteCount int
				if err := totRows.Scan(&optText, &voteCount); err == nil {
					votesMap[optText] = voteCount
				}
			}
			totRows.Close()
			notices[i].Votes = votesMap
		}

		// 3. Fetch current user's cast vote
		var myVoteText string
		myVoteQuery := `
			SELECT o.option_text 
			FROM notice_votes v
			JOIN notice_options o ON v.option_id = o.id
			WHERE v.notice_id = $1 AND v.user_id = $2`
		err = app.DB.QueryRow(c.Request.Context(), myVoteQuery, n.ID, userUID).Scan(&myVoteText)
		if err == nil {
			notices[i].MyVote = &myVoteText
		}
	}

	c.JSON(http.StatusOK, notices)
}

// GetNoticeByID retrieves a single notice with options and votes details.
func (app *App) GetNoticeByID(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	noticeID := c.Param("noticeId")

	// Verify requester is a member of the club
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Fetch notice
	var n NoticeResp
	query := `
		SELECT n.id, n.club_id, n.title, n.message, n.is_event, n.created_at, COALESCE(u.name, 'System')
		FROM notices n
		LEFT JOIN users u ON n.created_by = u.id
		WHERE n.id = $1 AND n.club_id = $2`

	err = app.DB.QueryRow(c.Request.Context(), query, noticeID, clubID).Scan(
		&n.ID, &n.ClubID, &n.Title, &n.Message, &n.IsEvent, &n.CreatedAt, &n.CreatedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Notice not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch notice: " + err.Error()})
		return
	}

	n.Options = []string{}
	n.Votes = make(map[string]int)

	// Fetch options
	optRows, err := app.DB.Query(c.Request.Context(), `SELECT option_text FROM notice_options WHERE notice_id = $1`, n.ID)
	if err == nil {
		options := []string{}
		for optRows.Next() {
			var opt string
			if err := optRows.Scan(&opt); err == nil {
				options = append(options, opt)
			}
		}
		optRows.Close()
		n.Options = options
	}

	// Fetch vote totals
	totalsQuery := `
		SELECT o.option_text, COUNT(v.user_id) 
		FROM notice_options o
		LEFT JOIN notice_votes v ON o.id = v.option_id
		WHERE o.notice_id = $1
		GROUP BY o.option_text`
	totRows, err := app.DB.Query(c.Request.Context(), totalsQuery, n.ID)
	if err == nil {
		votesMap := make(map[string]int)
		for totRows.Next() {
			var optText string
			var voteCount int
			if err := totRows.Scan(&optText, &voteCount); err == nil {
				votesMap[optText] = voteCount
			}
		}
		totRows.Close()
		n.Votes = votesMap
	}

	// Fetch current user's cast vote
	var myVoteText string
	myVoteQuery := `
		SELECT o.option_text 
		FROM notice_votes v
		JOIN notice_options o ON v.option_id = o.id
		WHERE v.notice_id = $1 AND v.user_id = $2`
	err = app.DB.QueryRow(c.Request.Context(), myVoteQuery, n.ID, userUID).Scan(&myVoteText)
	if err == nil {
		n.MyVote = &myVoteText
	}

	c.JSON(http.StatusOK, n)
}

// DeleteNotice removes a notice by ID.
func (app *App) DeleteNotice(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	noticeID := c.Param("noticeId")

	// Verify requester is club owner or admin
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if status != "active" || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Owner or Admin permissions required."})
		return
	}

	query := `DELETE FROM notices WHERE id = $1 AND club_id = $2`
	result, err := app.DB.Exec(c.Request.Context(), query, noticeID, clubID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete notice: " + err.Error()})
		return
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Notice record not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Notice deleted successfully"})
}

