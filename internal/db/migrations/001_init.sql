-- Initiales Datenbankschema (v1 inkl. v2-Vorbereitung für Benutzerkonten/Rollen)

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'editor', 'reader')),
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS articles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slug TEXT NOT NULL UNIQUE,
    template_type TEXT NOT NULL CHECK (template_type IN ('problem_solution', 'howto')),
    title TEXT NOT NULL,
    category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'active', 'outdated')),
    error_codes TEXT NOT NULL DEFAULT '',
    symptoms TEXT NOT NULL DEFAULT '',
    solution_body TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]',
    author_name TEXT NOT NULL DEFAULT '',
    author_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    version INTEGER NOT NULL DEFAULT 1,
    last_verified_at TEXT,
    last_verified_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_articles_category ON articles(category_id);
CREATE INDEX IF NOT EXISTS idx_articles_status ON articles(status);
CREATE INDEX IF NOT EXISTS idx_articles_deleted ON articles(deleted_at);
CREATE INDEX IF NOT EXISTS idx_articles_updated ON articles(updated_at DESC);

CREATE TABLE IF NOT EXISTS article_revisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    template_type TEXT NOT NULL,
    title TEXT NOT NULL,
    category_id INTEGER REFERENCES categories(id) ON DELETE SET NULL,
    status TEXT NOT NULL,
    error_codes TEXT NOT NULL DEFAULT '',
    symptoms TEXT NOT NULL DEFAULT '',
    solution_body TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]',
    editor_name TEXT NOT NULL DEFAULT '',
    editor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    change_summary TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE(article_id, version)
);

CREATE INDEX IF NOT EXISTS idx_revisions_article ON article_revisions(article_id, version DESC);

CREATE TABLE IF NOT EXISTS synonym_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    terms_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS media_files (
    id TEXT PRIMARY KEY,
    stored_path TEXT NOT NULL UNIQUE,
    original_name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('image', 'video', 'attachment')),
    size_bytes INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    uploaded_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_media_sha256 ON media_files(sha256);
