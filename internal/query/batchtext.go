package query

import (
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
)

const operationIndent = "  "

// FormatOperation writes op as the batch grammar spells it, so that parsing
// the text gives op back.
func FormatOperation(op adapter.Operation) string {
	words := []string{strings.ToUpper(string(op.Kind))}
	if op.Kind != adapter.OperationCreate && op.Kind != adapter.OperationUpsert {
		words = append(words, string(jsonString(op.ID)))
	}
	if len(op.Body) > 0 {
		words = append(words, string(op.Body))
	}
	if op.Condition != "" {
		words = append(words, "WHERE", string(jsonString(op.Condition)))
	}
	if op.IfMatch != "" {
		words = append(words, "IF MATCH", string(jsonString(op.IfMatch)))
	}
	return strings.Join(words, " ")
}

// FormatBatch writes b as one statement, an operation to a line.
func FormatBatch(b adapter.Batch) string {
	keys := make([]string, 0, len(b.PartitionKey))
	for _, value := range b.PartitionKey {
		keys = append(keys, string(value))
	}
	var text strings.Builder
	text.WriteString("BEGIN BATCH " + strings.Join(b.Scope, ".") + " PARTITION " + strings.Join(keys, ", ") + ";\n")
	for _, op := range b.Operations {
		text.WriteString(operationIndent + FormatOperation(op) + ";\n")
	}
	text.WriteString("COMMIT")
	return text.String()
}

// AppendOperation adds op as the last operation of the batch text holds,
// leaving the rest of the text as it was written. Text that is not a batch
// that parses is refused.
func AppendOperation(text string, op adapter.Operation) (string, error) {
	if _, err := ParseBatch(text); err != nil {
		return "", err
	}
	commit := lastKeyword(code(lex(text)), "COMMIT")
	lineStart := strings.LastIndexByte(text[:commit], '\n') + 1
	line := operationIndent + FormatOperation(op) + ";\n"
	if strings.TrimSpace(text[lineStart:commit]) != "" {
		return text[:commit] + "\n" + line + text[commit:], nil
	}
	return text[:lineStart] + line + text[lineStart:], nil
}

func lastKeyword(toks []token, keyword string) int {
	for i := len(toks) - 1; i >= 0; i-- {
		if keywordAt(toks, i, keyword) {
			return toks[i].start
		}
	}
	return -1
}

// BatchPrefix is the longest start of a batch's text, at most limit bytes,
// that ends with an operation's semicolon. Cut there, the text has lost its
// COMMIT, so what is kept can be read but never run.
func BatchPrefix(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	end := 0
	for _, tok := range code(lex(text)) {
		if tok.end > limit || tok.kind == tokIdent && tok.upper == "COMMIT" {
			break
		}
		if isSymbol(tok, ";") {
			end = tok.end
		}
	}
	return text[:end]
}
