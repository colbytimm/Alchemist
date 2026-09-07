package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

// prompter reads a secret at the terminal. Piped input gets no label, only
// the answer read, so a script can feed a key without a prompt landing in its
// output.
type prompter struct {
	in  *bufio.Reader
	tty *os.File // set when the input is a terminal, where a secret must not echo
	out io.Writer
}

func newPrompter(cmd *cobra.Command) prompter {
	in := cmd.InOrStdin()
	p := prompter{in: bufio.NewReader(in), out: cmd.ErrOrStderr()}
	if file, ok := in.(*os.File); ok && term.IsTerminal(file.Fd()) {
		p.tty = file
	}
	return p
}

// secret reads one line, without echo on a terminal. End of input is an
// empty answer.
func (p prompter) secret(label string) (string, error) {
	if p.tty == nil {
		return p.line()
	}
	if err := p.show(label); err != nil {
		return "", err
	}
	raw, err := term.ReadPassword(p.tty.Fd())
	if err != nil {
		return "", fmt.Errorf("cmd: read secret: %w", err)
	}
	// The terminal swallowed the newline along with the secret.
	if err := p.show("\n"); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func (p prompter) show(text string) error {
	if _, err := io.WriteString(p.out, text); err != nil {
		return fmt.Errorf("cmd: write prompt: %w", err)
	}
	return nil
}

func (p prompter) line() (string, error) {
	text, err := p.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("cmd: read input: %w", err)
	}
	return strings.TrimSpace(text), nil
}
