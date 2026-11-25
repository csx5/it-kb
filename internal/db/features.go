package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/csx5/it-kb/internal/model"
)

// --- TOTP 2FA ---

func (s *Store) SetUserTOTPSecret(ctx context.Context, userID int64, secret string, enabled bool) error {
	var res sql.Result
	var err error
	if enabled {
		res, err = s.sqlDB.ExecContext(ctx, `
			UPDATE users SET totp_secret = ?, totp_enabled = 1 WHERE id = ?
		`, secret, userID)
	} else {
		// Beim Deaktivieren oder Neu-Setup auch den letzten Zeitschritt zurücksetzen
		res, err = s.sqlDB.ExecContext(ctx, `
			UPDATE users SET totp_secret = ?, totp_enabled = 0, last_totp_step = 0 WHERE id = ?
		`, secret, userID)
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ConsumeUserTOTPStep verbraucht einen 30s-TOTP-Zeitschritt atomar in SQLite (Replay-Schutz über Neustarts hinweg).
func (s *Store) ConsumeUserTOTPStep(ctx context.Context, userID, step int64) (bool, error) {
	if step <= 0 {
		return false, nil
	}
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE users SET last_totp_step = ?
		WHERE id = ? AND last_totp_step < ?
	`, step, userID, step)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
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

// --- Eigene Artikel-Vorlagen / Textbausteine ---

func normalizeTemplateInput(in model.ArticleTemplate) (model.ArticleTemplate, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, errors.New("vorlagenname darf nicht leer sein")
	}
	if len([]rune(in.Name)) > 120 {
		return in, errors.New("vorlagenname ist zu lang (max. 120 zeichen)")
	}
	if in.TemplateType != model.TemplateHowTo {
		in.TemplateType = model.TemplateProblemSolution
	}
	seen := make(map[string]struct{})
	cleanTags := make([]string, 0, len(in.Tags))
	for _, t := range in.Tags {
		tag := strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		cleanTags = append(cleanTags, tag)
	}
	in.Tags = cleanTags
	if in.SortOrder == 0 {
		in.SortOrder = 50
	}
	return in, nil
}

func (s *Store) ListArticleTemplates(ctx context.Context) ([]model.ArticleTemplate, error) {
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT id, name, template_type, title_prefix, error_codes, symptoms, solution_body, tags_json, sort_order, created_at, updated_at
		FROM article_templates
		ORDER BY sort_order ASC, name COLLATE NOCASE ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.ArticleTemplate, 0)
	for rows.Next() {
		var tpl model.ArticleTemplate
		var tagsJSON, createdStr, updatedStr string
		if err := rows.Scan(
			&tpl.ID, &tpl.Name, &tpl.TemplateType, &tpl.TitlePrefix,
			&tpl.ErrorCodes, &tpl.Symptoms, &tpl.SolutionBody, &tagsJSON,
			&tpl.SortOrder, &createdStr, &updatedStr,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &tpl.Tags)
		if tpl.Tags == nil {
			tpl.Tags = []string{}
		}
		tpl.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		tpl.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		out = append(out, tpl)
	}
	return out, rows.Err()
}

func (s *Store) CreateArticleTemplate(ctx context.Context, in model.ArticleTemplate) (model.ArticleTemplate, error) {
	norm, err := normalizeTemplateInput(in)
	if err != nil {
		return model.ArticleTemplate{}, err
	}
	tagsRaw, _ := json.Marshal(norm.Tags)
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	res, err := s.sqlDB.ExecContext(ctx, `
		INSERT INTO article_templates (name, template_type, title_prefix, error_codes, symptoms, solution_body, tags_json, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, norm.Name, norm.TemplateType, norm.TitlePrefix, norm.ErrorCodes, norm.Symptoms, norm.SolutionBody, string(tagsRaw), norm.SortOrder, nowStr, nowStr)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return model.ArticleTemplate{}, errors.New("eine vorlage mit diesem namen existiert bereits")
		}
		return model.ArticleTemplate{}, err
	}
	id, _ := res.LastInsertId()
	norm.ID = id
	norm.CreatedAt = now
	norm.UpdatedAt = now
	return norm, nil
}

func (s *Store) UpdateArticleTemplate(ctx context.Context, id int64, in model.ArticleTemplate) (model.ArticleTemplate, error) {
	norm, err := normalizeTemplateInput(in)
	if err != nil {
		return model.ArticleTemplate{}, err
	}
	tagsRaw, _ := json.Marshal(norm.Tags)
	now := time.Now().UTC()

	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE article_templates
		SET name = ?, template_type = ?, title_prefix = ?, error_codes = ?, symptoms = ?, solution_body = ?, tags_json = ?, sort_order = ?, updated_at = ?
		WHERE id = ?
	`, norm.Name, norm.TemplateType, norm.TitlePrefix, norm.ErrorCodes, norm.Symptoms, norm.SolutionBody, string(tagsRaw), norm.SortOrder, now.Format(time.RFC3339), id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return model.ArticleTemplate{}, errors.New("eine vorlage mit diesem namen existiert bereits")
		}
		return model.ArticleTemplate{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.ArticleTemplate{}, ErrNotFound
	}
	norm.ID = id
	norm.UpdatedAt = now
	return norm, nil
}

func (s *Store) DeleteArticleTemplate(ctx context.Context, id int64) error {
	res, err := s.sqlDB.ExecContext(ctx, `DELETE FROM article_templates WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
