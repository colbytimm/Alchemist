package emulator_test

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/colbytimm/alchemist/internal/emulator"
)

// fakeExec records every argv it is handed and answers from replies, keyed
// by the command's verb: "run", "container inspect", and so on. A verb with
// no reply succeeds with no output.
type fakeExec struct {
	calls   [][]string
	replies map[string][]reply
}

type reply struct {
	out string
	err error
}

func newFakeExec() *fakeExec { return &fakeExec{replies: map[string][]reply{}} }

// on queues answers for verb; the last one repeats once the queue is spent.
func (f *fakeExec) on(verb string, replies ...reply) *fakeExec {
	f.replies[verb] = append(f.replies[verb], replies...)
	return f
}

func (f *fakeExec) Output(_ context.Context, args []string) ([]byte, error) {
	f.calls = append(f.calls, args)
	r := f.next(verbOf(args))
	return []byte(r.out), r.err
}

func (f *fakeExec) Stream(_ context.Context, args []string, w io.Writer) error {
	f.calls = append(f.calls, args)
	r := f.next(verbOf(args))
	if _, err := io.WriteString(w, r.out); err != nil {
		return err
	}
	return r.err
}

func (f *fakeExec) next(verb string) reply {
	queue := f.replies[verb]
	if len(queue) == 0 {
		return reply{}
	}
	if len(queue) > 1 {
		f.replies[verb] = queue[1:]
	}
	return queue[0]
}

// verbs lists the verb of every call made, in order.
func (f *fakeExec) verbs() []string {
	var verbs []string
	for _, args := range f.calls {
		verbs = append(verbs, verbOf(args))
	}
	return verbs
}

func verbOf(args []string) string {
	if len(args) > 1 && (args[0] == "container" || args[0] == "image" || args[0] == "volume") {
		return args[0] + " " + args[1]
	}
	return args[0]
}

func docker(exec *fakeExec) emulator.Runtime {
	return emulator.Runtime{Name: "docker", Exec: exec}
}

func failure(stderr string) reply {
	return reply{err: &emulator.CommandError{Stderr: stderr, Err: fmt.Errorf("exit status 1")}}
}

var noSuchContainer = failure("Error: No such container: " + emulator.ContainerName)

func inspected(state string, managed bool, hostPort int, imageID string) reply {
	labels := `{}`
	if managed {
		labels = `{"` + emulator.Label + `":"1"}`
	}
	return reply{out: fmt.Sprintf(`[{"Image":%q,"State":{"Status":%q,"ExitCode":%d},"Config":{"Labels":%s},`+
		`"HostConfig":{"PortBindings":{"8081/tcp":[{"HostIp":"127.0.0.1","HostPort":"%d"}]}}}]`,
		imageID, state, exitCodeOf(state), labels, hostPort)}
}

func exitCodeOf(state string) int {
	if strings.HasPrefix(state, "exited") {
		return 1
	}
	return 0
}
