package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/csx5/it-kb/internal/model"
)

func (s *Store) InsertMediaFile(ctx context.Context, m model.MediaFile) error {
	_, err := s.sqlDB.ExecContext(ctx, `
		INSERT INTO media_files (
			id, stored_path, original_name, mime_type, kind, size_bytes, sha256, uploaded_by, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, m.ID, m.StoredPath, m.OriginalName, m.MimeType, m.Kind, m.SizeBytes, m.SHA256, m.UploadedBy, m.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) GetMediaFile(ctx context.Context, id string) (model.MediaFile, error) {
	var m model.MediaFile
	var createdStr string
	err := s.sqlDB.QueryRowContext(ctx, `
		SELECT id, stored_path, original_name, mime_type, kind, size_bytes, sha256, uploaded_by, created_at
		FROM media_files
		WHERE id = ?
	`, id).Scan(&m.ID, &m.StoredPath, &m.OriginalName, &m.MimeType, &m.Kind, &m.SizeBytes, &m.SHA256, &m.UploadedBy, &createdStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return m, ErrNotFound
		}
		return m, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return m, nil
}

// ListOrphanedMediaFiles liefert alle Mediendateien älter als minAge, die weder in einem Artikel
// noch in irgendeiner gespeicherten Revision referenziert werden.
func (s *Store) ListOrphanedMediaFiles(ctx context.Context, minAge time.Duration) ([]model.MediaFile, error) {
	// Alle Inhalte aus Artikeln und Revisionen sammeln
	var corpus strings.Builder
	for _, query := range []string{
		`SELECT symptoms, solution_body FROM articles`,
		`SELECT symptoms, solution_body FROM article_revisions`,
	} {
		rows, err := s.sqlDB.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var sym, sol string
			if err := rows.Scan(&sym, &sol); err != nil {
				rows.Close()
				return nil, err
			}
			corpus.WriteString(sym)
			corpus.WriteByte('\n')
			corpus.WriteString(sol)
			corpus.WriteByte('\n')
		}
		rows.Close()
	}
	allText := corpus.String()

	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT id, stored_path, original_name, mime_type, kind, size_bytes, sha256, uploaded_by, created_at
		FROM media_files
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cutoff := time.Now().UTC().Add(-minAge)
	var orphans []model.MediaFile
	for rows.Next() {
		var m model.MediaFile
		var createdStr string
		if err := rows.Scan(&m.ID, &m.StoredPath, &m.OriginalName, &m.MimeType, &m.Kind, &m.SizeBytes, &m.SHA256, &m.UploadedBy, &createdStr); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		if minAge > 0 && m.CreatedAt.After(cutoff) {
			continue
		}
		if !strings.Contains(allText, "/media/"+m.ID) {
			orphans = append(orphans, m)
		}
	}
	return orphans, rows.Err()
}

func (s *Store) DeleteMediaFile(ctx context.Context, id string) error {
	_, err := s.sqlDB.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, id)
	return err
}
