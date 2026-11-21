package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/csx5/it-kb/internal/model"
)

var (
	ErrUserExists = errors.New("benutzername ist bereits vergeben")
	ErrLastAdmin  = errors.New("der letzte aktive administrator darf nicht entfernt oder herabgestuft werden")
)

type UserWithHash struct {
	model.User
	PasswordHash string
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND is_active = 1`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, displayName, passwordHash, role string) (model.User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	displayName = strings.TrimSpace(displayName)
	if len(username) < 2 || len(username) > 48 || strings.ContainsAny(username, " \t\r\n") {
		return model.User{}, errors.New("benutzername muss 2 bis 48 zeichen ohne leerzeichen enthalten")
	}
	if displayName == "" {
		displayName = username
	}
	if !isValidRole(role) {
		return model.User{}, errors.New("ungültige rolle (erlaubt: admin, editor, reader)")
	}

	now := time.Now().UTC()
	res, err := s.sqlDB.ExecContext(ctx, `
		INSERT INTO users (username, display_name, password_hash, role, is_active, created_at)
		VALUES (?, ?, ?, ?, 1, ?)
	`, username, displayName, passwordHash, role, now.Format(time.RFC3339))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return model.User{}, ErrUserExists
		}
		return model.User{}, fmt.Errorf("benutzer anlegen: %w", err)
	}
	id, _ := res.LastInsertId()
	return model.User{
		ID:          id,
		Username:    username,
		DisplayName: displayName,
		Role:        role,
		IsActive:    true,
		CreatedAt:   now,
	}, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (UserWithHash, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	var u UserWithHash
	var activeInt int
	var createdStr string
	err := s.sqlDB.QueryRowContext(ctx, `
		SELECT id, username, display_name, password_hash, role, is_active, created_at
		FROM users WHERE username = ?
	`, username).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.Role, &activeInt, &createdStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, ErrNotFound
		}
		return u, err
	}
	u.IsActive = activeInt == 1
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (UserWithHash, error) {
	var u UserWithHash
	var activeInt int
	var createdStr string
	err := s.sqlDB.QueryRowContext(ctx, `
		SELECT id, username, display_name, password_hash, role, is_active, created_at
		FROM users WHERE id = ?
	`, id).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.Role, &activeInt, &createdStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, ErrNotFound
		}
		return u, err
	}
	u.IsActive = activeInt == 1
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT id, username, display_name, role, is_active, created_at
		FROM users
		ORDER BY CASE role WHEN 'admin' THEN 1 WHEN 'editor' THEN 2 ELSE 3 END, username ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.User
	for rows.Next() {
		var u model.User
		var activeInt int
		var createdStr string
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &activeInt, &createdStr); err != nil {
			return nil, err
		}
		u.IsActive = activeInt == 1
		u.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUser(ctx context.Context, id int64, displayName, role string, isActive bool) (model.User, error) {
	existing, err := s.GetUserByID(ctx, id)
	if err != nil {
		return model.User{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = existing.Username
	}
	if !isValidRole(role) {
		return model.User{}, errors.New("ungültige rolle")
	}

	// Schutz vor Aussperrung: Letzter aktiver Admin darf nicht deaktiviert oder herabgestuft werden
	if existing.Role == model.RoleAdmin && existing.IsActive && (role != model.RoleAdmin || !isActive) {
		admins, err := s.CountActiveAdmins(ctx)
		if err != nil {
			return model.User{}, err
		}
		if admins <= 1 {
			return model.User{}, ErrLastAdmin
		}
	}

	activeInt := 0
	if isActive {
		activeInt = 1
	}
	if _, err := s.sqlDB.ExecContext(ctx, `
		UPDATE users SET display_name = ?, role = ?, is_active = ? WHERE id = ?
	`, displayName, role, activeInt, id); err != nil {
		return model.User{}, err
	}

	// Bei Deaktivierung sofort alle offenen Sessions dieses Benutzers beenden
	if !isActive {
		_ = s.DeleteUserSessions(ctx, id)
	}

	updated, err := s.GetUserByID(ctx, id)
	return updated.User, err
}

func (s *Store) UpdateUserPassword(ctx context.Context, id int64, newPasswordHash string, revokeSessions bool) error {
	res, err := s.sqlDB.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, newPasswordHash, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if revokeSessions {
		_ = s.DeleteUserSessions(ctx, id)
	}
	return nil
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	existing, err := s.GetUserByID(ctx, id)
	if err != nil {
		return err
	}
	if existing.Role == model.RoleAdmin && existing.IsActive {
		admins, err := s.CountActiveAdmins(ctx)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return ErrLastAdmin
		}
	}
	_, err = s.sqlDB.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.sqlDB.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)
	`, tokenHash, userID, expiresAt.UTC().Format(time.RFC3339), now)
	return err
}

func (s *Store) GetSessionUser(ctx context.Context, tokenHash string) (model.User, error) {
	var u model.User
	var activeInt int
	var createdStr, expiresStr string
	err := s.sqlDB.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.display_name, u.role, u.is_active, u.created_at, s.expires_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?
	`, tokenHash).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &activeInt, &createdStr, &expiresStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, ErrNotFound
		}
		return u, err
	}

	expiresAt, err := time.Parse(time.RFC3339, expiresStr)
	if err != nil || time.Now().UTC().After(expiresAt) || activeInt != 1 {
		_ = s.DeleteSession(ctx, tokenHash)
		return model.User{}, ErrNotFound
	}
	u.IsActive = true
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return u, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.sqlDB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := s.sqlDB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *Store) CleanupExpiredSessions(ctx context.Context) error {
	_, err := s.sqlDB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC().Format(time.RFC3339))
	return err
}

// --- Audit-Log ---

func (s *Store) WriteAuditLog(ctx context.Context, actorUserID *int64, actorName, action, target, ipAddr, details string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.sqlDB.ExecContext(ctx, `
		INSERT INTO audit_log (created_at, actor_user_id, actor_name, action, target, ip_address, details)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, now, nullableInt64(actorUserID), actorName, action, target, ipAddr, details)
}

func (s *Store) ListAuditLogs(ctx context.Context, limit int) ([]model.AuditLogEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT id, created_at, actor_user_id, actor_name, action, target, ip_address, details
		FROM audit_log
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.AuditLogEntry
	for rows.Next() {
		var e model.AuditLogEntry
		var createdStr string
		var uid sql.NullInt64
		if err := rows.Scan(&e.ID, &createdStr, &uid, &e.ActorName, &e.Action, &e.Target, &e.IPAddress, &e.Details); err != nil {
			return nil, err
		}
		if uid.Valid {
			v := uid.Int64
			e.ActorUserID = &v
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		out = append(out, e)
	}
	return out, rows.Err()
}

func isValidRole(r string) bool {
	return r == model.RoleAdmin || r == model.RoleEditor || r == model.RoleReader
}
