package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/csx5/it-kb/internal/model"
)

var (
	ErrNotFound        = errors.New("artikel nicht gefunden")
	ErrVersionConflict = errors.New("versionskonflikt beim speichern")
)

type CreateArticleInput struct {
	TemplateType string
	Title        string
	CategoryID   *int64
	Status       string
	ErrorCodes   string
	Symptoms     string
	SolutionBody string
	Tags         []string
	AuthorName   string
	AuthorUserID *int64
}

type UpdateArticleInput struct {
	ExpectedVersion int
	Force           bool
	TemplateType    string
	Title           string
	CategoryID      *int64
	Status          string
	ErrorCodes      string
	Symptoms        string
	SolutionBody    string
	Tags            []string
	EditorName      string
	EditorUserID    *int64
	ChangeSummary   string
}

func (s *Store) CreateArticle(ctx context.Context, in CreateArticleInput) (model.Article, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return model.Article{}, errors.New("titel darf nicht leer sein")
	}
	if in.TemplateType != model.TemplateProblemSolution && in.TemplateType != model.TemplateHowTo {
		in.TemplateType = model.TemplateProblemSolution
	}
	if in.Status != model.StatusDraft && in.Status != model.StatusActive && in.Status != model.StatusOutdated {
		in.Status = model.StatusActive
	}
	tags := normalizeTags(in.Tags)
	tagsJSON, _ := json.Marshal(tags)
	author := strings.TrimSpace(in.AuthorName)
	if author == "" {
		author = "Unbekannt"
	}

	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return model.Article{}, err
	}
	defer func() { _ = tx.Rollback() }()

	slug, err := uniqueSlugTx(ctx, tx, in.Title)
	if err != nil {
		return model.Article{}, err
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO articles (
			slug, template_type, title, category_id, status,
			error_codes, symptoms, solution_body, tags_json,
			author_name, author_user_id, version, last_verified_at, last_verified_by,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)
	`, slug, in.TemplateType, in.Title, nullableInt64(in.CategoryID), in.Status,
		strings.TrimSpace(in.ErrorCodes), in.Symptoms, in.SolutionBody, string(tagsJSON),
		author, nullableInt64(in.AuthorUserID), now, author, now, now)
	if err != nil {
		return model.Article{}, fmt.Errorf("artikel anlegen: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Article{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO article_revisions (
			article_id, version, template_type, title, category_id, status,
			error_codes, symptoms, solution_body, tags_json,
			editor_name, editor_user_id, change_summary, created_at
		) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'Erstellt', ?)
	`, id, in.TemplateType, in.Title, nullableInt64(in.CategoryID), in.Status,
		strings.TrimSpace(in.ErrorCodes), in.Symptoms, in.SolutionBody, string(tagsJSON),
		author, nullableInt64(in.AuthorUserID), now); err != nil {
		return model.Article{}, fmt.Errorf("erste revision anlegen: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.Article{}, err
	}
	return s.GetArticleByID(ctx, id)
}

// UpdateArticle aktualisiert einen Artikel mit Optimistic Locking.
// Bei Versionskonflikt wird ErrVersionConflict zusammen mit dem aktuellen Server-Stand zurückgegeben.
func (s *Store) UpdateArticle(ctx context.Context, id int64, in UpdateArticleInput) (model.Article, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return model.Article{}, errors.New("titel darf nicht leer sein")
	}
	if in.TemplateType != model.TemplateProblemSolution && in.TemplateType != model.TemplateHowTo {
		in.TemplateType = model.TemplateProblemSolution
	}
	if in.Status != model.StatusDraft && in.Status != model.StatusActive && in.Status != model.StatusOutdated {
		in.Status = model.StatusActive
	}
	tags := normalizeTags(in.Tags)
	tagsJSON, _ := json.Marshal(tags)
	editor := strings.TrimSpace(in.EditorName)
	if editor == "" {
		editor = "Unbekannt"
	}
	summary := strings.TrimSpace(in.ChangeSummary)

	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return model.Article{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var currentVersion int
	var deletedAt sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT version, deleted_at FROM articles WHERE id = ?`, id).Scan(&currentVersion, &deletedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Article{}, ErrNotFound
		}
		return model.Article{}, err
	}
	if deletedAt.Valid && deletedAt.String != "" {
		return model.Article{}, errors.New("artikel befindet sich im papierkorb und muss zuerst wiederhergestellt werden")
	}

	if !in.Force && in.ExpectedVersion != currentVersion {
		_ = tx.Rollback()
		latest, getErr := s.GetArticleByID(ctx, id)
		if getErr != nil {
			return model.Article{}, ErrVersionConflict
		}
		return latest, ErrVersionConflict
	}

	nextVersion := currentVersion + 1
	now := time.Now().UTC().Format(time.RFC3339)

	if _, err := tx.ExecContext(ctx, `
		UPDATE articles
		SET template_type = ?, title = ?, category_id = ?, status = ?,
			error_codes = ?, symptoms = ?, solution_body = ?, tags_json = ?,
			version = ?, updated_at = ?
		WHERE id = ?
	`, in.TemplateType, in.Title, nullableInt64(in.CategoryID), in.Status,
		strings.TrimSpace(in.ErrorCodes), in.Symptoms, in.SolutionBody, string(tagsJSON),
		nextVersion, now, id); err != nil {
		return model.Article{}, fmt.Errorf("artikel aktualisieren: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO article_revisions (
			article_id, version, template_type, title, category_id, status,
			error_codes, symptoms, solution_body, tags_json,
			editor_name, editor_user_id, change_summary, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, nextVersion, in.TemplateType, in.Title, nullableInt64(in.CategoryID), in.Status,
		strings.TrimSpace(in.ErrorCodes), in.Symptoms, in.SolutionBody, string(tagsJSON),
		editor, nullableInt64(in.EditorUserID), summary, now); err != nil {
		return model.Article{}, fmt.Errorf("revision anlegen: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.Article{}, err
	}
	return s.GetArticleByID(ctx, id)
}

func (s *Store) VerifyArticle(ctx context.Context, id int64, verifier string) (model.Article, error) {
	verifier = strings.TrimSpace(verifier)
	if verifier == "" {
		verifier = "Unbekannt"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE articles
		SET last_verified_at = ?, last_verified_by = ?
		WHERE id = ? AND deleted_at IS NULL
	`, now, verifier, id)
	if err != nil {
		return model.Article{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.Article{}, ErrNotFound
	}
	return s.GetArticleByID(ctx, id)
}

func (s *Store) UnverifyArticle(ctx context.Context, id int64) (model.Article, error) {
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE articles
		SET last_verified_at = NULL, last_verified_by = ''
		WHERE id = ? AND deleted_at IS NULL
	`, id)
	if err != nil {
		return model.Article{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.Article{}, ErrNotFound
	}
	return s.GetArticleByID(ctx, id)
}

func (s *Store) SoftDeleteArticle(ctx context.Context, id int64) (model.Article, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE articles
		SET deleted_at = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, now, now, id)
	if err != nil {
		return model.Article{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.Article{}, ErrNotFound
	}
	return s.GetArticleByID(ctx, id)
}

func (s *Store) RestoreArticle(ctx context.Context, id int64) (model.Article, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE articles
		SET deleted_at = NULL, updated_at = ?
		WHERE id = ? AND deleted_at IS NOT NULL
	`, now, id)
	if err != nil {
		return model.Article{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.Article{}, ErrNotFound
	}
	return s.GetArticleByID(ctx, id)
}

func (s *Store) PermanentDeleteArticle(ctx context.Context, id int64) error {
	res, err := s.sqlDB.ExecContext(ctx, `DELETE FROM articles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func normalizeTags(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "#")))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func nullableInt64(v *int64) any {
	if v == nil || *v <= 0 {
		return nil
	}
	return *v
}

func uniqueSlugTx(ctx context.Context, tx *sql.Tx, title string) (string, error) {
	base := slugify(title)
	if base == "" {
		base = "artikel"
	}
	candidate := base
	for i := 2; i < 1000; i++ {
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles WHERE slug = ?`, candidate).Scan(&exists)
		if err != nil {
			return "", err
		}
		if exists == 0 {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano()), nil
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	r := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")
	s = r.Replace(s)

	var b strings.Builder
	lastDash := false
	for _, ch := range s {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			b.WriteRune(ch)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
		if b.Len() >= 64 {
			break
		}
	}
	return strings.TrimRight(b.String(), "-")
}
