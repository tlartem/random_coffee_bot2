package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Participant struct {
	ID        uuid.UUID
	GroupID   int64
	UserID    int64
	Username  string
	FullName  string
	CreatedAt time.Time
}

type Pair struct {
	ID        uuid.UUID
	GroupID   int64
	WeekStart string
	User1ID   int64
	User2ID   int64
	CreatedAt time.Time
}

type PollMapping struct {
	PollID    string
	GroupID   int64
	MessageID int64
}

// Participant operations

func CreateOrUpdateParticipant(ctx context.Context, db *sql.DB, p Participant) error {
	query := `INSERT INTO participant (id, group_id, user_id, username, full_name, created_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT (group_id, user_id) DO UPDATE
	SET username = EXCLUDED.username, full_name = EXCLUDED.full_name`

	_, err := db.ExecContext(ctx, query, p.ID.String(), p.GroupID, p.UserID, p.Username, p.FullName, p.CreatedAt)
	return err
}

func GetAllParticipants(ctx context.Context, db *sql.DB, groupID int64) ([]Participant, error) {
	query := `SELECT id, group_id, user_id, username, full_name, created_at
	FROM participant WHERE group_id = ?`

	rows, err := db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	participants := make([]Participant, 0)
	for rows.Next() {
		var p Participant
		var idStr string
		if err := rows.Scan(&idStr, &p.GroupID, &p.UserID, &p.Username, &p.FullName, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.ID, _ = uuid.Parse(idStr)
		participants = append(participants, p)
	}
	return participants, nil
}

func DeleteParticipant(ctx context.Context, db *sql.DB, groupID, userID int64) error {
	query := `DELETE FROM participant WHERE group_id = ? AND user_id = ?`
	_, err := db.ExecContext(ctx, query, groupID, userID)
	return err
}

func ClearAllParticipants(ctx context.Context, db *sql.DB, groupID int64) error {
	query := `DELETE FROM participant WHERE group_id = ?`
	_, err := db.ExecContext(ctx, query, groupID)
	return err
}

// Pair operations

func CreatePairs(ctx context.Context, db *sql.DB, pairs []Pair) error {
	if len(pairs) == 0 {
		return nil
	}

	query := `INSERT INTO pair (id, group_id, week_start, user1_id, user2_id, created_at)
	VALUES (?, ?, ?, ?, ?, ?)`

	for _, p := range pairs {
		if _, err := db.ExecContext(ctx, query, p.ID.String(), p.GroupID, p.WeekStart, p.User1ID, p.User2ID, p.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

func GetAvailablePairs(ctx context.Context, db *sql.DB, groupID int64) ([][2]Participant, error) {
	query := `
	WITH available_users AS (
		SELECT
			p1.id as p1_id, p1.user_id as p1_user_id, p1.username as p1_username,
			p1.full_name as p1_full_name, p1.created_at as p1_created_at,
			p2.id as p2_id, p2.user_id as p2_user_id, p2.username as p2_username,
			p2.full_name as p2_full_name, p2.created_at as p2_created_at
		FROM participant p1
		CROSS JOIN participant p2
		WHERE p1.group_id = ? AND p2.group_id = ? AND p1.user_id < p2.user_id
	)
	SELECT p1_id, p1_user_id, p1_username, p1_full_name, p1_created_at,
	       p2_id, p2_user_id, p2_username, p2_full_name, p2_created_at
	FROM available_users au
	WHERE NOT EXISTS (
		SELECT 1 FROM pair pr
		WHERE pr.group_id = ?
		  AND ((pr.user1_id = au.p1_user_id AND pr.user2_id = au.p2_user_id)
			OR (pr.user1_id = au.p2_user_id AND pr.user2_id = au.p1_user_id))
	)
	ORDER BY RANDOM()`

	rows, err := db.QueryContext(ctx, query, groupID, groupID, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pairs := make([][2]Participant, 0)
	for rows.Next() {
		var p1, p2 Participant
		var p1IDStr, p2IDStr string
		p1.GroupID = groupID
		p2.GroupID = groupID

		if err := rows.Scan(&p1IDStr, &p1.UserID, &p1.Username, &p1.FullName, &p1.CreatedAt,
			&p2IDStr, &p2.UserID, &p2.Username, &p2.FullName, &p2.CreatedAt); err != nil {
			return nil, err
		}
		p1.ID, _ = uuid.Parse(p1IDStr)
		p2.ID, _ = uuid.Parse(p2IDStr)
		pairs = append(pairs, [2]Participant{p1, p2})
	}
	return pairs, nil
}

// Poll mapping operations

func CreatePollMapping(ctx context.Context, db *sql.DB, pm PollMapping) error {
	query := `INSERT INTO poll_mapping (poll_id, group_id, message_id) VALUES (?, ?, ?)`
	_, err := db.ExecContext(ctx, query, pm.PollID, pm.GroupID, pm.MessageID)
	return err
}

func GetGroupIDByPollID(ctx context.Context, db *sql.DB, pollID string) (int64, error) {
	query := `SELECT group_id FROM poll_mapping WHERE poll_id = ?`

	var groupID int64
	err := db.QueryRowContext(ctx, query, pollID).Scan(&groupID)
	if err != nil {
		return 0, fmt.Errorf("poll not found: %w", err)
	}
	return groupID, nil
}

func GetPollMappingByGroupID(ctx context.Context, db *sql.DB, groupID int64) (*PollMapping, error) {
	query := `SELECT poll_id, group_id, message_id FROM poll_mapping WHERE group_id = ? ORDER BY rowid DESC LIMIT 1`

	var pm PollMapping
	err := db.QueryRowContext(ctx, query, groupID).Scan(&pm.PollID, &pm.GroupID, &pm.MessageID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get poll mapping: %w", err)
	}
	return &pm, nil
}

func DeletePollMapping(ctx context.Context, db *sql.DB, groupID int64) error {
	query := `DELETE FROM poll_mapping WHERE group_id = ?`
	_, err := db.ExecContext(ctx, query, groupID)
	return err
}
