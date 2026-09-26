package query

import "slices"

// SourcePaths lists the dotted path of every FROM and JOIN source in text, in
// order, CTE bodies included. A CTE read by its name is not a source.
func SourcePaths(text string) [][]string {
	ctes := cteNames(text)
	var paths [][]string
	for _, s := range parse(text).sources {
		if len(s.path) == 1 && slices.Contains(ctes, s.path[0].text) {
			continue
		}
		paths = append(paths, pathText(s.path))
	}
	return paths
}

// cteNames are the names text's WITH clause declares, if it parses.
func cteNames(text string) []string {
	stmt, err := parseStatement(text)
	if err != nil {
		return nil
	}
	return stmt.names()
}
