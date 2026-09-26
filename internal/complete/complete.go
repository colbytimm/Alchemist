// Package complete ranks what the editor can offer at a cursor: keywords,
// Cosmos system functions, and the databases, containers, and fields the
// session has seen so far. It makes no request of any backend; the TUI feeds
// the index from what it fetched and asks it after every keystroke.
package complete

import (
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

type Kind int

const (
	KindKeyword Kind = iota + 1
	KindFunction
	KindDatabase
	KindContainer
	KindAlias
	KindField
)

// Suggestion is one thing the cursor may become. Insert differs from Text for
// a function, which opens its parentheses, and for a field that is not a
// plain identifier, which is written in bracket form.
type Suggestion struct {
	Text   string
	Insert string
	Kind   Kind
	Detail string
	// Bracketed marks a bracket-form Insert, which replaces the dot before
	// the word as well as the word.
	Bracketed bool
}

// Detail texts for what the catalog serves.
const (
	detailDatabase     = "database"
	detailContainer    = "container"
	detailField        = "field"
	detailAlias        = "alias"
	detailScope        = "scope"
	detailPartitionKey = "partition key"
	detailSeparator    = " · "
)

// scopeAlias is the bare alias a query reads the catalog's scope through.
const scopeAlias = "c"

// Index is what a session knows names for. It is fed by plain method calls
// and mutated in place, so the model shares one.
type Index struct {
	scope      []string
	databases  []string
	containers map[string][]string
	fields     map[string]*fieldSet
}

func NewIndex() *Index {
	return &Index{containers: map[string][]string{}, fields: map[string]*fieldSet{}}
}

// SetScope records the container the catalog selected, which a bare alias
// reads.
func (x *Index) SetScope(scope []string) {
	x.scope = slices.Clone(scope)
}

// SetDatabases replaces the top level of the catalog, forgetting the
// containers and fields of every database no longer in it, and reports the
// containers it forgot.
func (x *Index) SetDatabases(names []string) (dropped [][]string) {
	x.databases = slices.Clone(names)
	for database := range x.containers {
		if !slices.Contains(names, database) {
			dropped = append(dropped, x.dropDatabase(database)...)
		}
	}
	return dropped
}

func (x *Index) dropDatabase(database string) (dropped [][]string) {
	for _, container := range x.containers[database] {
		dropped = append(dropped, x.dropContainer([]string{database, container}))
	}
	delete(x.containers, database)
	return dropped
}

func (x *Index) dropContainer(container []string) []string {
	delete(x.fields, containerKey(container))
	return container
}

// SetContainers replaces the containers of database, forgetting the fields of
// any it no longer holds and filing each one's partition key as a field. It
// reports the containers it forgot.
func (x *Index) SetContainers(database string, containers []adapter.Node) (dropped [][]string) {
	names := make([]string, 0, len(containers))
	for _, container := range containers {
		names = append(names, container.Name)
	}
	for _, container := range x.containers[database] {
		if !slices.Contains(names, container) {
			dropped = append(dropped, x.dropContainer([]string{database, container}))
		}
	}
	x.containers[database] = names
	for _, container := range containers {
		x.fieldsOf(container.Path).setPartitionKey(container.Meta[adapter.MetaPartitionKey])
	}
	return dropped
}

func (x *Index) HasContainers(database string) bool {
	_, ok := x.containers[database]
	return ok
}

func (x *Index) AddFields(container []string, fields []adapter.Field) {
	x.fieldsOf(container).add(fields)
}

func (x *Index) fieldsOf(container []string) *fieldSet {
	key := containerKey(container)
	set, ok := x.fields[key]
	if !ok {
		set = newFieldSet()
		x.fields[key] = set
	}
	return set
}

// Suggest ranks what fits c: case-insensitive prefix matches on the word
// typed so far, then substring matches, each group in the order the index
// holds them.
func (x *Index) Suggest(c query.Completion) []Suggestion {
	return rank(x.candidates(c), c.Word)
}

func (x *Index) candidates(c query.Completion) []Suggestion {
	switch c.Kind {
	case query.CompleteKeyword:
		return keywordSuggestions(c.Keywords, c.Word)
	case query.CompleteSource:
		return append(x.databaseSuggestions(), x.scopeSuggestion()...)
	case query.CompleteDatabase:
		return x.databaseSuggestions()
	case query.CompleteContainer:
		return x.containerSuggestions(c.Database)
	case query.CompleteField:
		return x.fieldSuggestions(c)
	case query.CompleteReference:
		return x.referenceSuggestions(c)
	case query.CompleteExpression:
		return slices.Concat(x.referenceSuggestions(c), functionSuggestions(), keywordSuggestions(c.Keywords, c.Word))
	}
	return nil
}

func functionSuggestions() []Suggestion {
	functions := query.Functions()
	suggestions := make([]Suggestion, 0, len(functions))
	for _, f := range functions {
		suggestions = append(suggestions, Suggestion{Text: f.Name, Insert: f.Name + "(", Kind: KindFunction, Detail: f.Signature})
	}
	return suggestions
}

func keywordSuggestions(keywords []string, word string) []Suggestion {
	suggestions := make([]Suggestion, 0, len(keywords))
	for _, keyword := range keywords {
		text := keywordCase(word, keyword)
		suggestions = append(suggestions, Suggestion{Text: text, Insert: text, Kind: KindKeyword})
	}
	return suggestions
}

// keywordCase follows the case the word is being typed in, so a query
// written in lowercase stays that way.
func keywordCase(word, keyword string) string {
	if word != "" && word == strings.ToLower(word) {
		return strings.ToLower(keyword)
	}
	return keyword
}

func (x *Index) databaseSuggestions() []Suggestion {
	suggestions := make([]Suggestion, 0, len(x.databases))
	for _, name := range x.databases {
		suggestions = append(suggestions, Suggestion{Text: name, Insert: name, Kind: KindDatabase, Detail: detailDatabase})
	}
	return suggestions
}

func (x *Index) scopeSuggestion() []Suggestion {
	if len(x.scope) == 0 {
		return nil
	}
	return []Suggestion{{
		Text:   scopeAlias,
		Insert: scopeAlias,
		Kind:   KindAlias,
		Detail: detailScope + detailSeparator + scopeLabel(x.scope),
	}}
}

func (x *Index) containerSuggestions(database string) []Suggestion {
	containers := x.containers[database]
	suggestions := make([]Suggestion, 0, len(containers))
	for _, name := range containers {
		suggestions = append(suggestions, Suggestion{Text: name, Insert: name, Kind: KindContainer, Detail: detailContainer})
	}
	return suggestions
}

// fieldSuggestions lists what sits under the alias at the typed path. The
// SELECT list of a cross-container join projects top-level fields only, so
// nothing is offered below one there.
func (x *Index) fieldSuggestions(c query.Completion) []Suggestion {
	if len(c.Aliases) == 0 || c.TopLevel && len(c.Path) > 0 {
		return nil
	}
	alias := c.Aliases[0]
	var suggestions []Suggestion
	for _, field := range x.fieldsUnder(alias, c.Path) {
		if c.Writable && !field.writable {
			continue
		}
		suggestion := Suggestion{Text: field.name, Insert: field.name, Kind: KindField, Detail: field.detail}
		if !isIdentifier(field.name) {
			suggestion.Insert, suggestion.Bracketed = bracketed(field.name), true
		}
		suggestions = append(suggestions, suggestion)
	}
	return suggestions
}

// referenceSuggestions lists every alias, then the fields of each written
// as alias.field.
func (x *Index) referenceSuggestions(c query.Completion) []Suggestion {
	var suggestions []Suggestion
	for _, alias := range c.Aliases {
		suggestions = append(suggestions, Suggestion{
			Text: alias.Name, Insert: alias.Name, Kind: KindAlias, Detail: aliasDetail(alias),
		})
	}
	for _, alias := range c.Aliases {
		for _, field := range x.fieldsUnder(alias, nil) {
			if c.Writable && !field.writable {
				continue
			}
			text := alias.Name + "." + field.name
			if !isIdentifier(field.name) {
				text = alias.Name + bracketed(field.name)
			}
			suggestions = append(suggestions, Suggestion{Text: text, Insert: text, Kind: KindField, Detail: field.detail})
		}
	}
	return suggestions
}

func aliasDetail(alias query.Alias) string {
	if len(alias.Scopes) == 0 {
		return detailAlias
	}
	labels := make([]string, 0, len(alias.Scopes))
	for _, scope := range alias.Scopes {
		labels = append(labels, scopeLabel(scope))
	}
	detail := strings.Join(labels, ", ")
	if len(alias.Path) > 0 {
		detail += detailSeparator + strings.Join(alias.Path, ".")
	}
	return detail
}

// namedField is one child of a path, with the detail line describing it.
// writable is false for what an update may not write: the id, a partition
// key path or what holds one, and a field the service owns.
type namedField struct {
	name     string
	detail   string
	writable bool
}

// fieldsUnder merges the children of path across every container the alias
// is bound to, in the order they were first seen. A field some of several
// containers lack says which have it.
func (x *Index) fieldsUnder(alias query.Alias, path []string) []namedField {
	prefix := strings.Join(slices.Concat(alias.Path, path), ".")
	if prefix != "" {
		prefix += "."
	}
	var order []string
	seen := map[string]*childField{}
	for _, scope := range alias.Scopes {
		set, ok := x.fields[containerKey(scope)]
		if !ok {
			continue
		}
		for _, child := range set.children(prefix) {
			merged, known := seen[child.name]
			if !known {
				merged = &childField{name: child.name}
				seen[child.name] = merged
				order = append(order, child.name)
			}
			merged.merge(child, scope[len(scope)-1])
		}
	}
	fields := make([]namedField, 0, len(order))
	for _, name := range order {
		field := seen[name]
		fields = append(fields, namedField{
			name:     name,
			detail:   field.detail(len(alias.Scopes)),
			writable: !field.holdsKey && !unwritableRoot(prefix, name),
		})
	}
	return fields
}

// unwritableRoot reports a top-level field no update may write: the id,
// or one the backend sets on every item.
func unwritableRoot(prefix, name string) bool {
	return prefix == "" && (name == "id" || adapter.IsSystemField(name))
}

// childField is one direct child of a path, as one or several containers
// hold it. The kind is empty until an item has shown it, and again once
// items disagree about it.
type childField struct {
	name         string
	kind         string
	observed     bool
	partitionKey bool
	holdsKey     bool
	containers   []string
}

func (f *childField) merge(other childField, container string) {
	switch {
	case !other.observed:
	case !f.observed:
		f.kind, f.observed = other.kind, true
	case other.kind != f.kind:
		f.kind = ""
	}
	f.partitionKey = f.partitionKey || other.partitionKey
	f.holdsKey = f.holdsKey || other.holdsKey
	f.containers = append(f.containers, container)
}

func (f *childField) detail(scopes int) string {
	detail := f.kind
	if detail == "" {
		detail = detailField
	}
	if f.partitionKey {
		detail += detailSeparator + detailPartitionKey
	}
	if scopes > 1 && len(f.containers) < scopes {
		detail += detailSeparator + strings.Join(f.containers, ", ")
	}
	return detail
}

func rank(candidates []Suggestion, word string) []Suggestion {
	lower := strings.ToLower(word)
	var prefixed, contained []Suggestion
	for _, candidate := range candidates {
		text := strings.ToLower(candidate.Text)
		switch {
		case strings.HasPrefix(text, lower):
			prefixed = append(prefixed, candidate)
		case strings.Contains(text, lower):
			contained = append(contained, candidate)
		}
	}
	return append(prefixed, contained...)
}

func scopeLabel(scope []string) string {
	return strings.Join(scope, ".")
}

// containerKey joins a path with a byte no backend identifier can hold.
func containerKey(container []string) string {
	return strings.Join(container, "\x00")
}

func bracketed(name string) string {
	return `["` + strings.ReplaceAll(name, `"`, `\"`) + `"]`
}

func isIdentifier(name string) bool {
	for i, r := range name {
		letter := r == '_' || r == '$' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		digit := r >= '0' && r <= '9'
		if !letter && (i == 0 || !digit) {
			return false
		}
	}
	return name != ""
}
