ALTER TABLE users ADD COLUMN last_totp_step INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS article_templates (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL UNIQUE,
    template_type TEXT NOT NULL DEFAULT 'problem_solution',
    title_prefix  TEXT NOT NULL DEFAULT '',
    error_codes   TEXT NOT NULL DEFAULT '',
    symptoms      TEXT NOT NULL DEFAULT '',
    solution_body TEXT NOT NULL DEFAULT '',
    tags_json     TEXT NOT NULL DEFAULT '[]',
    sort_order    INTEGER NOT NULL DEFAULT 50,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
