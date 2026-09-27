package theme

// Role is what a color is for. A theme gives every role a color; panes ask
// for styles by role and never name a color.
type Role int

const (
	Text Role = iota
	Muted
	Accent
	Selected
	Success
	Warning
	Error
	Keyword
	Operator
	Literal
	Function
	Alias
	Parameter
	String
	Number
	Comment
	Punctuation
	roleCount
)

// roleNames are the keys of a theme file's [colors] table.
var roleNames = [roleCount]string{
	Text:        "text",
	Muted:       "muted",
	Accent:      "accent",
	Selected:    "selected",
	Success:     "success",
	Warning:     "warning",
	Error:       "error",
	Keyword:     "keyword",
	Operator:    "operator",
	Literal:     "literal",
	Function:    "function",
	Alias:       "alias",
	Parameter:   "parameter",
	String:      "string",
	Number:      "number",
	Comment:     "comment",
	Punctuation: "punctuation",
}

func Roles() []Role {
	roles := make([]Role, roleCount)
	for i := range roles {
		roles[i] = Role(i)
	}
	return roles
}

// String is the role's key in a theme file.
func (r Role) String() string {
	return roleNames[r]
}
