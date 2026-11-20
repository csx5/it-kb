package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/csx5/it-kb/internal/model"
)

func (s *Store) ListArticleRevisions(ctx context.Context, articleID int64) ([]model.ArticleRevision, error) {
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT
			id, article_id, version, template_type, title, category_id, status,
			error_codes, symptoms, solution_body, tags_json,
			editor_name, editor_user_id, change_summary, created_at
		FROM article_revisions
		WHERE article_id = ?
		ORDER BY version DESC
	`, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.ArticleRevision
	for rows.Next() {
		var r model.ArticleRevision
		var catID, editorUID sql.NullInt64
		var tagsJSON, createdStr string
		if err := rows.Scan(
			&r.ID, &r.ArticleID, &r.Version, &r.TemplateType, &r.Title, &catID, &r.Status,
			&r.ErrorCodes, &r.Symptoms, &r.SolutionBody, &tagsJSON,
			&r.EditorName, &editorUID, &r.ChangeSummary, &createdStr,
		); err != nil {
			return nil, err
		}
		if catID.Valid {
			v := catID.Int64
			r.CategoryID = &v
		}
		if editorUID.Valid {
			v := editorUID.Int64
			r.EditorUserID = &v
		}
		_ = json.Unmarshal([]byte(tagsJSON), &r.Tags)
		if r.Tags == nil {
			r.Tags = []string{}
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RestoreArticleRevision(ctx context.Context, articleID int64, targetVersion int, editorName string) (model.Article, error) {
	var rev model.ArticleRevision
	var catID sql.NullInt64
	var tagsJSON string

	err := s.sqlDB.QueryRowContext(ctx, `
		SELECT template_type, title, category_id, status, error_codes, symptoms, solution_body, tags_json
		FROM article_revisions
		WHERE article_id = ? AND version = ?
	`, articleID, targetVersion).Scan(
		&rev.TemplateType, &rev.Title, &catID, &rev.Status,
		&rev.ErrorCodes, &rev.Symptoms, &rev.SolutionBody, &tagsJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Article{}, ErrNotFound
		}
		return model.Article{}, err
	}
	if catID.Valid {
		v := catID.Int64
		rev.CategoryID = &v
	}
	_ = json.Unmarshal([]byte(tagsJSON), &rev.Tags)

	return s.UpdateArticle(ctx, articleID, UpdateArticleInput{
		Force:         true,
		TemplateType:  rev.TemplateType,
		Title:         rev.Title,
		CategoryID:    rev.CategoryID,
		Status:        rev.Status,
		ErrorCodes:    rev.ErrorCodes,
		Symptoms:      rev.Symptoms,
		SolutionBody:  rev.SolutionBody,
		Tags:          rev.Tags,
		EditorName:    editorName,
		ChangeSummary: fmt.Sprintf("Wiederhergestellt aus Version %d", targetVersion),
	})
}

func (s *Store) CreateCategory(ctx context.Context, name, description string, sortOrder int) (model.Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Category{}, errors.New("kategoriename darf nicht leer sein")
	}
	slug := slugify(name)
	if slug == "" {
		slug = fmt.Sprintf("cat-%d", time.Now().Unix())
	}
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := s.sqlDB.ExecContext(ctx, `
		INSERT INTO categories (slug, name, description, sort_order, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, slug, name, strings.TrimSpace(description), sortOrder, now)
	if err != nil {
		return model.Category{}, fmt.Errorf("kategorie anlegen (evtl. Name bereits vergeben): %w", err)
	}
	id, _ := res.LastInsertId()
	return model.Category{
		ID:          id,
		Slug:        slug,
		Name:        name,
		Description: strings.TrimSpace(description),
		SortOrder:   sortOrder,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (s *Store) UpdateCategory(ctx context.Context, id int64, name, description string, sortOrder int) (model.Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Category{}, errors.New("kategoriename darf nicht leer sein")
	}
	slug := slugify(name)
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE categories SET slug = ?, name = ?, description = ?, sort_order = ?
		WHERE id = ?
	`, slug, name, strings.TrimSpace(description), sortOrder, id)
	if err != nil {
		return model.Category{}, fmt.Errorf("kategorie aktualisieren: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.Category{}, ErrNotFound
	}
	return model.Category{
		ID:          id,
		Slug:        slug,
		Name:        name,
		Description: strings.TrimSpace(description),
		SortOrder:   sortOrder,
	}, nil
}

func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	res, err := s.sqlDB.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateSynonymGroup(ctx context.Context, terms []string) (model.SynonymGroup, error) {
	cleaned := cleanSynonymTerms(terms)
	if len(cleaned) < 2 {
		return model.SynonymGroup{}, errors.New("eine synonym-gruppe benötigt mindestens 2 begriffe")
	}
	raw, _ := json.Marshal(cleaned)
	now := time.Now().UTC()
	res, err := s.sqlDB.ExecContext(ctx, `
		INSERT INTO synonym_groups (terms_json, updated_at) VALUES (?, ?)
	`, string(raw), now.Format(time.RFC3339))
	if err != nil {
		return model.SynonymGroup{}, err
	}
	id, _ := res.LastInsertId()
	return model.SynonymGroup{ID: id, Terms: cleaned, UpdatedAt: now}, nil
}

func (s *Store) UpdateSynonymGroup(ctx context.Context, id int64, terms []string) (model.SynonymGroup, error) {
	cleaned := cleanSynonymTerms(terms)
	if len(cleaned) < 2 {
		return model.SynonymGroup{}, errors.New("eine synonym-gruppe benötigt mindestens 2 begriffe")
	}
	raw, _ := json.Marshal(cleaned)
	now := time.Now().UTC()
	res, err := s.sqlDB.ExecContext(ctx, `
		UPDATE synonym_groups SET terms_json = ?, updated_at = ? WHERE id = ?
	`, string(raw), now.Format(time.RFC3339), id)
	if err != nil {
		return model.SynonymGroup{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.SynonymGroup{}, ErrNotFound
	}
	return model.SynonymGroup{ID: id, Terms: cleaned, UpdatedAt: now}, nil
}

func (s *Store) DeleteSynonymGroup(ctx context.Context, id int64) error {
	res, err := s.sqlDB.ExecContext(ctx, `DELETE FROM synonym_groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) EmptyTrash(ctx context.Context) ([]int64, error) {
	rows, err := s.sqlDB.QueryContext(ctx, `SELECT id FROM articles WHERE deleted_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	if len(ids) == 0 {
		return nil, nil
	}
	if _, err := s.sqlDB.ExecContext(ctx, `DELETE FROM articles WHERE deleted_at IS NOT NULL`); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *Store) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.sqlDB.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.sqlDB.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

func cleanSynonymTerms(terms []string) []string {
	seen := make(map[string]struct{}, len(terms))
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.ToLower(strings.TrimSpace(t))
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
