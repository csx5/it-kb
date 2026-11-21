package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/csx5/it-kb/internal/model"
)

// --- TOTP 2FA ---

func (s *Store) SetUserTOTPSecret(ctx context.Context, userID int64, secret string, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE users SET totp_secret = ?, totp_enabled = ? WHERE id = ?
	`, secret, en, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Hilfreich-Zähler & Angepinnt ---

func (s *Store) IncrementArticleHelpful(ctx context.Context, articleID int64) (model.Article, error) {
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE articles SET helpful_count = helpful_count + 1
		WHERE id = ? AND deleted_at IS NULL
	`, articleID)
	if err != nil {
		return model.Article{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.Article{}, ErrNotFound
	}
	return s.GetArticleByID(ctx, articleID)
}

func (s *Store) SetArticlePinned(ctx context.Context, articleID int64, pinned bool) (model.Article, error) {
	v := 0
	if pinned {
		v = 1
	}
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE articles SET is_pinned = ?
		WHERE id = ? AND deleted_at IS NULL
	`, v, articleID)
	if err != nil {
		return model.Article{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.Article{}, ErrNotFound
	}
	return s.GetArticleByID(ctx, articleID)
}

// --- Praxis-Kommentare ---

func (s *Store) ListArticleComments(ctx context.Context, articleID int64) ([]model.ArticleComment, error) {
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT id, article_id, author_name, author_user_id, body, created_at
		FROM article_comments
		WHERE article_id = ?
		ORDER BY created_at ASC, id ASC
	`, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.ArticleComment
	for rows.Next() {
		var c model.ArticleComment
		var uid sql.NullInt64
		var createdStr string
		if err := rows.Scan(&c.ID, &c.ArticleID, &c.AuthorName, &uid, &c.Body, &createdStr); err != nil {
			return nil, err
		}
		if uid.Valid {
			v := uid.Int64
			c.AuthorUserID = &v
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CreateArticleComment(ctx context.Context, articleID int64, authorName string, authorUserID *int64, body string) (model.ArticleComment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return model.ArticleComment{}, errors.New("kommentar darf nicht leer sein")
	}
	if len([]rune(body)) > 4000 {
		return model.ArticleComment{}, errors.New("kommentar ist zu lang (max. 4000 zeichen)")
	}
	authorName = strings.TrimSpace(authorName)
	if authorName == "" {
		authorName = "Unbekannt"
	}

	var exists int
	if err := s.sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles WHERE id = ? AND deleted_at IS NULL`, articleID).Scan(&exists); err != nil {
		return model.ArticleComment{}, err
	}
	if exists == 0 {
		return model.ArticleComment{}, ErrNotFound
	}

	now := time.Now().UTC()
	res, err := s.sqlDB.ExecContext(ctx, `
		INSERT INTO article_comments (article_id, author_name, author_user_id, body, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, articleID, authorName, nullableInt64(authorUserID), body, now.Format(time.RFC3339))
	if err != nil {
		return model.ArticleComment{}, err
	}
	id, _ := res.LastInsertId()
	return model.ArticleComment{
		ID:           id,
		ArticleID:    articleID,
		AuthorName:   authorName,
		AuthorUserID: authorUserID,
		Body:         body,
		CreatedAt:    now,
	}, nil
}

func (s *Store) GetArticleComment(ctx context.Context, commentID int64) (model.ArticleComment, error) {
	var c model.ArticleComment
	var uid sql.NullInt64
	var createdStr string
	err := s.sqlDB.QueryRowContext(ctx, `
		SELECT id, article_id, author_name, author_user_id, body, created_at
		FROM article_comments WHERE id = ?
	`, commentID).Scan(&c.ID, &c.ArticleID, &c.AuthorName, &uid, &c.Body, &createdStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, ErrNotFound
		}
		return c, err
	}
	if uid.Valid {
		v := uid.Int64
		c.AuthorUserID = &v
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return c, nil
}

func (s *Store) DeleteArticleComment(ctx context.Context, commentID int64) error {
	res, err := s.sqlDB.ExecContext(ctx, `DELETE FROM article_comments WHERE id = ?`, commentID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
