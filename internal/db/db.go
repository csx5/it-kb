package db

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/csx5/it-kb/internal/model"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	dbPath string
	sqlDB  *sql.DB
}

func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, fmt.Errorf("datenverzeichnis anlegen: %w", err)
	}

	sqlDB, err := openSQLite(dbPath)
	if err != nil {
		return nil, err
	}

	store := &Store{dbPath: dbPath, sqlDB: sqlDB}
	if err := store.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return store, nil
}

func openSQLite(dbPath string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)", dbPath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite öffnen: %w", err)
	}
	// SQLite mit einem Writer-Pool stabil betreiben
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("sqlite ping: %w", err)
	}
	return sqlDB, nil
}

// ReplaceFromFile schließt die laufende SQLite-Verbindung (wichtig u. a. für Windows),
// ersetzt die Datenbankdatei atomar mit Rollback-Sicherung und öffnet sie neu.
func (s *Store) ReplaceFromFile(snapshotPath string) error {
	raw, err := os.ReadFile(snapshotPath)
	if err != nil {
		return fmt.Errorf("snapshot lesen: %w", err)
	}

	if s.sqlDB != nil {
		_ = s.sqlDB.Close()
		s.sqlDB = nil
	}
	_ = os.Remove(s.dbPath + "-wal")
	_ = os.Remove(s.dbPath + "-shm")

	prevPath := s.dbPath + ".rollback"
	hasPrev := false
	if oldRaw, err := os.ReadFile(s.dbPath); err == nil {
		if writeErr := os.WriteFile(prevPath, oldRaw, 0o600); writeErr == nil {
			hasPrev = true
		}
	}
	defer func() {
		if hasPrev {
			_ = os.Remove(prevPath)
		}
	}()

	rollback := func(cause error) error {
		if hasPrev {
			if oldRaw, rErr := os.ReadFile(prevPath); rErr == nil {
				_ = os.WriteFile(s.dbPath, oldRaw, 0o640)
				if reopened, oErr := openSQLite(s.dbPath); oErr == nil {
					s.sqlDB = reopened
				}
			}
		}
		return cause
	}

	if err := os.WriteFile(s.dbPath, raw, 0o640); err != nil {
		return rollback(fmt.Errorf("datenbankdatei überschreiben: %w", err))
	}

	sqlDB, err := openSQLite(s.dbPath)
	if err != nil {
		return rollback(err)
	}
	s.sqlDB = sqlDB
	if err := s.migrate(); err != nil {
		_ = s.sqlDB.Close()
		s.sqlDB = nil
		return rollback(err)
	}
	return nil
}

func (s *Store) Close() error {
	if s.sqlDB == nil {
		return nil
	}
	return s.sqlDB.Close()
}

func (s *Store) DB() *sql.DB {
	return s.sqlDB
}

func (s *Store) migrate() error {
	steps := []struct {
		version int
		file    string
	}{
		{1, "migrations/001_init.sql"},
		{2, "migrations/002_auth_audit.sql"},
		{3, "migrations/003_v3_features.sql"},
		{4, "migrations/004_v110_features.sql"},
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, st := range steps {
		// Bei Migration > 1 prüfen, ob sie bereits angewendet wurde (wichtig für ALTER TABLE)
		if st.version > 1 {
			var applied int
			if err := s.sqlDB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, st.version).Scan(&applied); err == nil && applied > 0 {
				continue
			}
		}

		raw, err := migrationsFS.ReadFile(st.file)
		if err != nil {
			return fmt.Errorf("migration %03d lesen: %w", st.version, err)
		}
		if _, err := s.sqlDB.Exec(string(raw)); err != nil {
			return fmt.Errorf("migration %03d ausführen: %w", st.version, err)
		}
		if _, err := s.sqlDB.Exec(`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (?, ?)`, st.version, now); err != nil {
			return err
		}
	}
	return nil
}

// VacuumInto erstellt einen konsistenten SQLite-Hot-Snapshot für Voll-Backups.
func (s *Store) VacuumInto(ctx context.Context, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
		return err
	}
	_ = os.Remove(targetPath)
	escaped := strings.ReplaceAll(targetPath, "'", "''")
	_, err := s.sqlDB.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", escaped))
	return err
}

func (s *Store) ListCategories(ctx context.Context) ([]model.Category, error) {
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT id, slug, name, description, sort_order, created_at
		FROM categories
		ORDER BY sort_order ASC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Category
	for rows.Next() {
		var c model.Category
		var createdStr string
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.Description, &c.SortOrder, &createdStr); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ListSynonymGroups(ctx context.Context) ([]model.SynonymGroup, error) {
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT id, terms_json, updated_at
		FROM synonym_groups
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.SynonymGroup
	for rows.Next() {
		var g model.SynonymGroup
		var termsJSON, updatedStr string
		if err := rows.Scan(&g.ID, &termsJSON, &updatedStr); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(termsJSON), &g.Terms)
		g.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) ListAllArticles(ctx context.Context) ([]model.Article, error) {
	rows, err := s.sqlDB.QueryContext(ctx, `
		SELECT
			a.id, a.slug, a.template_type, a.title, a.category_id,
			COALESCE(c.name, ''),
			a.status, a.error_codes, a.symptoms, a.solution_body,
			a.tags_json, a.author_name, a.author_user_id, a.version,
			a.helpful_count, a.is_pinned,
			a.last_verified_at, a.last_verified_by,
			a.created_at, a.updated_at, a.deleted_at
		FROM articles a
		LEFT JOIN categories c ON c.id = a.category_id
		ORDER BY a.is_pinned DESC, a.updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetArticleByID(ctx context.Context, id int64) (model.Article, error) {
	row := s.sqlDB.QueryRowContext(ctx, `
		SELECT
			a.id, a.slug, a.template_type, a.title, a.category_id,
			COALESCE(c.name, ''),
			a.status, a.error_codes, a.symptoms, a.solution_body,
			a.tags_json, a.author_name, a.author_user_id, a.version,
			a.helpful_count, a.is_pinned,
			a.last_verified_at, a.last_verified_by,
			a.created_at, a.updated_at, a.deleted_at
		FROM articles a
		LEFT JOIN categories c ON c.id = a.category_id
		WHERE a.id = ?
	`, id)
	return scanArticle(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanArticle(r rowScanner) (model.Article, error) {
	var a model.Article
	var catID, authorUID sql.NullInt64
	var pinnedInt int
	var verifiedAt, deletedAt sql.NullString
	var tagsJSON, createdStr, updatedStr string

	err := r.Scan(
		&a.ID, &a.Slug, &a.TemplateType, &a.Title, &catID,
		&a.CategoryName,
		&a.Status, &a.ErrorCodes, &a.Symptoms, &a.SolutionBody,
		&tagsJSON, &a.AuthorName, &authorUID, &a.Version,
		&a.HelpfulCount, &pinnedInt,
		&verifiedAt, &a.LastVerifiedBy,
		&createdStr, &updatedStr, &deletedAt,
	)
	if err != nil {
		return a, err
	}
	a.IsPinned = pinnedInt == 1
	if catID.Valid {
		v := catID.Int64
		a.CategoryID = &v
	}
	if authorUID.Valid {
		v := authorUID.Int64
		a.AuthorUserID = &v
	}
	_ = json.Unmarshal([]byte(tagsJSON), &a.Tags)
	if a.Tags == nil {
		a.Tags = []string{}
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	if verifiedAt.Valid && verifiedAt.String != "" {
		if t, err := time.Parse(time.RFC3339, verifiedAt.String); err == nil {
			a.LastVerifiedAt = &t
		}
	}
	if deletedAt.Valid && deletedAt.String != "" {
		if t, err := time.Parse(time.RFC3339, deletedAt.String); err == nil {
			a.DeletedAt = &t
		}
	}
	return a, nil
}
