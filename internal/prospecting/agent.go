package prospecting

const AnswerNo = "no"

const AnswerUnknown = "unknown"

type AddContactIn struct {
	Company     string `json:"company" jsonschema:"company id or exact name"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Email       string `json:"email,omitempty"`
	EmailStatus string `json:"email_status,omitempty" jsonschema:"verified when a page you cite shows the address, guess when inferred from a pattern, else empty"`
	Linkedin    string `json:"linkedin,omitempty" jsonschema:"profile URL, found by search, never scraped"`
	Phone       string `json:"phone,omitempty"`
	SourceURL   string `json:"source_url" jsonschema:"page that shows this person has this role (required)"`
	Task        int64  `json:"task,omitempty" jsonschema:"the id of the task this is for, if any"`
}
