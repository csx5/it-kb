package model

import "time"

const (
	TemplateProblemSolution = "problem_solution"
	TemplateHowTo           = "howto"

	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusOutdated = "outdated"

	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleReader = "reader"

	MediaKindImage      = "image"
	MediaKindVideo      = "video"
	MediaKindAttachment = "attachment"
)

type Category struct {
	ID          int64     `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
}

type Article struct {
	ID             int64      `json:"id"`
	Slug           string     `json:"slug"`
	TemplateType   string     `json:"template_type"`
	Title          string     `json:"title"`
	CategoryID     *int64     `json:"category_id,omitempty"`
	CategoryName   string     `json:"category_name,omitempty"`
	Status         string     `json:"status"`
	ErrorCodes     string     `json:"error_codes"`
	Symptoms       string     `json:"symptoms"`
	SolutionBody   string     `json:"solution_body"`
	Tags           []string   `json:"tags"`
	AuthorName     string     `json:"author_name"`
	AuthorUserID   *int64     `json:"author_user_id,omitempty"`
	Version        int        `json:"version"`
	HelpfulCount   int        `json:"helpful_count"`
	IsPinned       bool       `json:"is_pinned"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	LastVerifiedBy string     `json:"last_verified_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

type ArticleRevision struct {
	ID            int64     `json:"id"`
	ArticleID     int64     `json:"article_id"`
	Version       int       `json:"version"`
	TemplateType  string    `json:"template_type"`
	Title         string    `json:"title"`
	CategoryID    *int64    `json:"category_id,omitempty"`
	Status        string    `json:"status"`
	ErrorCodes    string    `json:"error_codes"`
	Symptoms      string    `json:"symptoms"`
	SolutionBody  string    `json:"solution_body"`
	Tags          []string  `json:"tags"`
	EditorName    string    `json:"editor_name"`
	EditorUserID  *int64    `json:"editor_user_id,omitempty"`
	ChangeSummary string    `json:"change_summary"`
	CreatedAt     time.Time `json:"created_at"`
}

type SynonymGroup struct {
	ID        int64     `json:"id"`
	Terms     []string  `json:"terms"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MediaFile struct {
	ID           string    `json:"id"`
	StoredPath   string    `json:"stored_path"`
	OriginalName string    `json:"original_name"`
	MimeType     string    `json:"mime_type"`
	Kind         string    `json:"kind"`
	SizeBytes    int64     `json:"size_bytes"`
	SHA256       string    `json:"sha256"`
	UploadedBy   string    `json:"uploaded_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// User und Session sind bereits für Version 2 (RBAC) vorbereitet.
type User struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	IsActive    bool      `json:"is_active"`
	TOTPEnabled bool      `json:"totp_enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

type ArticleComment struct {
	ID           int64     `json:"id"`
	ArticleID    int64     `json:"article_id"`
	AuthorName   string    `json:"author_name"`
	AuthorUserID *int64    `json:"author_user_id,omitempty"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
}

type AuditLogEntry struct {
	ID          int64     `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	ActorUserID *int64    `json:"actor_user_id,omitempty"`
	ActorName   string    `json:"actor_name"`
	Action      string    `json:"action"`
	Target      string    `json:"target"`
	IPAddress   string    `json:"ip_address"`
	Details     string    `json:"details"`
}

type SearchQuery struct {
	Text           string `json:"q"`
	CategoryID     *int64 `json:"category_id,omitempty"`
	Tag            string `json:"tag,omitempty"`
	Status         string `json:"status,omitempty"`
	TemplateType   string `json:"template_type,omitempty"`
	Author         string `json:"author,omitempty"`
	NeedsReview    bool   `json:"needs_review,omitempty"`
	IncludeDeleted bool   `json:"include_deleted,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type SearchHit struct {
	Article      Article `json:"article"`
	Score        float64 `json:"score"`
	SnippetHTML  string  `json:"snippet_html"`
	MatchedField string  `json:"matched_field"`
	NeedsReview  bool    `json:"needs_review"`
}
