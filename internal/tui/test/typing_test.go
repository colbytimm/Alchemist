package tui_test

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// numberedBuffer is a query of lines lines, its cursor on a number in the
// last one, where a digit or a backspace leaves no completion list open.
func numberedBuffer(lines int) string {
	rows := make([]string, 0, lines)
	rows = append(rows, "SELECT * FROM c WHERE c.a = 1")
	for i := range lines - 1 {
		rows = append(rows, fmt.Sprintf("  OR c.n = %d", i))
	}
	return strings.Join(rows, "\n")
}

func typedBuffer(b *testing.B, lines int) tea.Model {
	b.Helper()
	t := &testing.T{}
	m := typeQuery(t, selectContainer(t, newTallModel(t, newConnection(t))), numberedBuffer(lines))
	if listed(m.View(), "customerId") || !strings.Contains(plain(m.View()), fmt.Sprintf("OR c.n = %d", lines-2)) {
		b.Fatal("the buffer should be focused on its last line with no list open")
	}
	return m
}

// BenchmarkTypingPlainBuffer is one keystroke with no list open, and the frame
// that shows it. Keystrokes alternate a digit and a backspace, so the buffer
// never grows.
func BenchmarkTypingPlainBuffer(b *testing.B) {
	for _, lines := range []int{200, 2000} {
		b.Run(fmt.Sprintf("lines=%d", lines), func(b *testing.B) {
			m := typedBuffer(b, lines)
			keys := []tea.KeyMsg{keyRune('7'), keyMsg(tea.KeyBackspace)}
			b.ResetTimer()
			for i := range b.N {
				m, _ = m.Update(keys[i%len(keys)])
				_ = m.View()
			}
		})
	}
}

// BenchmarkViewWithoutEdit is a frame drawn for something other than an
// edit, a spinner tick say, over a buffer nothing has changed.
func BenchmarkViewWithoutEdit(b *testing.B) {
	for _, lines := range []int{200, 2000} {
		b.Run(fmt.Sprintf("lines=%d", lines), func(b *testing.B) {
			m := typedBuffer(b, lines)
			b.ResetTimer()
			for range b.N {
				_ = m.View()
			}
		})
	}
}
