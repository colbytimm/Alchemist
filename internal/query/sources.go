package query

// SourcePaths lists the dotted path of every FROM and JOIN source in text, in order.
func SourcePaths(text string) [][]string {
	p := parse(text)
	var paths [][]string
	for _, s := range p.sources {
		path := make([]string, 0, len(s.path))
		for _, part := range s.path {
			path = append(path, part.text)
		}
		paths = append(paths, path)
	}
	return paths
}
