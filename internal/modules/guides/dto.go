package guides

import "github.com/waste-not-app/waste-not-backend/internal/database"

type ListGuidesQuery struct {
	Query    string
	Category string
	Limit    int
	NamePos  string
	IDPos    string
}

type GuideRes struct {
	ID                string   `json:"id"`
	Slug              string   `json:"slug"`
	Name              string   `json:"name"`
	Category          string   `json:"category"`
	Keywords          []string `json:"keywords"`
	RecommendedMethod string   `json:"recommended_method"`
	Summary           string   `json:"summary"`
	Steps             []string `json:"steps"`
	Cautions          []string `json:"cautions"`
	SourceURL         string   `json:"source_url"`
	ReviewedAt        string   `json:"reviewed_at"`
	Reviewer          string   `json:"reviewer"`
	PublicationStatus string   `json:"publication_status"`
}

type ListGuidesRes struct {
	Items      []database.Guide
	HasMore    bool
	NextCursor any
}
