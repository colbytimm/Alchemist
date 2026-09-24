package query

// SourcePaths lists the dotted path of every FROM and JOIN source in text, in order.
func SourcePaths(text string) [][]string {
	p := parse(text)
	var paths [][]string
	for _, s := range p.sources {
		paths = append(paths, pathText(s.path))
	}
	return paths
}
