package skills

// Skill represents a single discovered skill with its metadata.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "global" or "project"
}

const (
	SourceGlobal  = "global"
	SourceProject = "project"
)
