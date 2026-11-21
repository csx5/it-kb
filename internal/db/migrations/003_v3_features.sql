-- Migration 003: TOTP 2FA, Hilfreich-Zähler, Angepinnte Artikel und Praxis-Kommentare

ALTER TABLE users ADD COLUMN totp_secret TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;

ALTER TABLE articles ADD COLUMN helpful_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE articles ADD COLUMN is_pinned INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS article_comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    author_name TEXT NOT NULL DEFAULT '',
    author_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    body TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_comments_article ON article_comments(article_id, created_at ASC);
