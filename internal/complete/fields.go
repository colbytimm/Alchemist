package complete

import (
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// fieldSet is every path seen in one container, in the order it was first
// seen, with the paths its partition key names.
type fieldSet struct {
	order         []string
	kinds         map[string]string // observed paths only; a path filed from the partition key has no kind yet
	partitionKeys map[string]bool
}

func newFieldSet() *fieldSet {
	return &fieldSet{kinds: map[string]string{}, partitionKeys: map[string]bool{}}
}

// setPartitionKey files the paths of a partition key as fields, so they
// complete before any item has been read.
func (s *fieldSet) setPartitionKey(joined string) {
	if joined == "" {
		return
	}
	for _, path := range strings.Split(joined, adapter.PartitionKeyPathSeparator) {
		field := strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", ".")
		s.partitionKeys[field] = true
		if !slices.Contains(s.order, field) {
			s.order = append(s.order, field)
		}
	}
}

// add files fields, keeping the kind of a path only while every observation
// agrees on it.
func (s *fieldSet) add(fields []adapter.Field) {
	for _, field := range fields {
		kind, observed := s.kinds[field.Path]
		switch {
		case !observed:
			if !s.partitionKeys[field.Path] {
				s.order = append(s.order, field.Path)
			}
			s.kinds[field.Path] = field.Kind
		case kind != field.Kind:
			s.kinds[field.Path] = ""
		}
	}
}

// children lists the direct children of prefix: the paths under it with no
// further nesting, an array's elements excluded since they have no name of
// their own.
func (s *fieldSet) children(prefix string) []childField {
	var children []childField
	for _, path := range s.order {
		name, ok := strings.CutPrefix(path, prefix)
		if !ok || name == "" || strings.ContainsAny(name, ".[") {
			continue
		}
		kind, observed := s.kinds[path]
		children = append(children, childField{
			name: name, kind: kind, observed: observed,
			partitionKey: s.partitionKeys[path], holdsKey: s.holdsKey(path),
		})
	}
	return children
}

// holdsKey reports whether path is a partition key path or holds one.
func (s *fieldSet) holdsKey(path string) bool {
	for key := range s.partitionKeys {
		if key == path || strings.HasPrefix(key, path+".") {
			return true
		}
	}
	return false
}
