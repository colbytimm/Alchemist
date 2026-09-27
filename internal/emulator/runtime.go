package emulator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// RuntimeEnvVar picks the runtime when --runtime does not.
const RuntimeEnvVar = "ALCHEMIST_CONTAINER_RUNTIME"

const (
	Docker = "docker"
	Podman = "podman"
)

// StateAbsent is the State of a container that does not exist.
const StateAbsent = "absent"

const statusLogLines = "20"

var ErrUnknownRuntime = errors.New("use docker or podman")

// Exec runs one command of the runtime's command line.
type Exec interface {
	// Output returns what the command wrote to stdout. A command that fails
	// returns a *CommandError.
	Output(ctx context.Context, args []string) ([]byte, error)
	// Stream copies the command's stdout and stderr to w as they arrive.
	Stream(ctx context.Context, args []string, w io.Writer) error
}

// CommandError is a runtime command that failed. Stderr is what it said.
type CommandError struct {
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	if line := firstLine(e.Stderr); line != "" {
		return line
	}
	return e.Err.Error()
}

func (e *CommandError) Unwrap() error { return e.Err }

// Runtime is the docker or podman command line. Every argv Alchemist hands
// either is built here.
type Runtime struct {
	Name string
	Exec Exec
}

// Container is what the runtime reports of the container named
// ContainerName. State is the runtime's own word for it, or StateAbsent.
type Container struct {
	State    string
	ExitCode int
	ImageID  string
	HostPort int
	// Managed is whether the container carries Label.
	Managed bool
}

func (c Container) Exists() bool { return c.State != StateAbsent }

func (c Container) Running() bool { return c.State == "running" || c.State == "restarting" }

// Stopped is a container that stopped by itself or was never started.
func (c Container) Stopped() bool { return c.State == "exited" || c.State == "dead" }

// DetectRuntime picks the runtime: choice when not empty, then
// RuntimeEnvVar, then docker if it is on PATH, then podman.
func DetectRuntime(choice string) (Runtime, error) {
	if choice == "" {
		choice = os.Getenv(RuntimeEnvVar)
	}
	if choice != "" {
		return namedRuntime(choice)
	}
	for _, name := range []string{Docker, Podman} {
		if path, err := exec.LookPath(name); err == nil {
			return Runtime{Name: name, Exec: CommandExec{Path: path}}, nil
		}
	}
	return Runtime{}, fmt.Errorf("emulator: %w: install Docker Desktop (https://docs.docker.com/get-docker/) or Podman, "+
		"then run alchemist emulator start%s", ErrNoRuntime, windowsEmulatorNote())
}

func namedRuntime(name string) (Runtime, error) {
	if name != Docker && name != Podman {
		return Runtime{}, fmt.Errorf("emulator: runtime %q: %w", name, ErrUnknownRuntime)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return Runtime{}, fmt.Errorf("emulator: %s is not on PATH", name)
	}
	return Runtime{Name: name, Exec: CommandExec{Path: path}}, nil
}

// RuntimeDownError is a runtime installed but unable to reach its daemon or,
// for Podman, its machine. Detail is the first line the runtime printed.
type RuntimeDownError struct {
	Runtime string
	Detail  string
}

func (e *RuntimeDownError) Error() string {
	if e.Runtime == Podman {
		return "emulator: podman cannot reach its machine: run podman machine start, then try again: " + e.Detail
	}
	return fmt.Sprintf("emulator: %s is installed but not running: start Docker Desktop or the docker service, then try again: %s",
		e.Runtime, e.Detail)
}

// Check fails with a *RuntimeDownError unless the runtime can reach its
// daemon: docker info and podman info both exit non-zero when it cannot.
func (r Runtime) Check(ctx context.Context) error {
	if _, err := r.Exec.Output(ctx, []string{"info"}); err != nil {
		return &RuntimeDownError{Runtime: r.Name, Detail: err.Error()}
	}
	return nil
}

// inspection is the part of container inspect's answer Container needs.
// Docker and Podman both answer in this shape.
type inspection struct {
	Image string
	State struct {
		Status   string
		ExitCode int
	}
	Config struct {
		Labels map[string]string
	}
	HostConfig struct {
		PortBindings map[string][]struct {
			HostPort string
		}
	}
}

func (r Runtime) Inspect(ctx context.Context) (Container, error) {
	out, err := r.Exec.Output(ctx, []string{"container", "inspect", ContainerName})
	if isNoSuchObject(err) {
		return Container{State: StateAbsent}, nil
	}
	if err != nil {
		return Container{}, r.commandError("container inspect", err)
	}
	var found []inspection
	if err := json.Unmarshal(out, &found); err != nil || len(found) != 1 {
		return Container{}, fmt.Errorf("emulator: %s container inspect: unexpected answer: %q", r.Name, firstLine(string(out)))
	}
	return found[0].container(), nil
}

// isNoSuchObject recognizes the runtimes' answer for a missing container:
// "No such container" from Docker, "no such container" from Podman.
func isNoSuchObject(err error) bool {
	var failed *CommandError
	return errors.As(err, &failed) && strings.Contains(strings.ToLower(failed.Stderr), "no such")
}

func (i inspection) container() Container {
	c := Container{
		State:    i.State.Status,
		ExitCode: i.State.ExitCode,
		ImageID:  strings.TrimPrefix(i.Image, "sha256:"),
		Managed:  i.Config.Labels[Label] == "1",
	}
	for _, binding := range i.HostConfig.PortBindings[strconv.Itoa(containerPort)+"/tcp"] {
		if port, err := strconv.Atoi(binding.HostPort); err == nil {
			c.HostPort = port
		}
	}
	return c
}

// ImageID is the ID of the local copy of Image, empty when there is none.
func (r Runtime) ImageID(ctx context.Context) (string, error) {
	out, err := r.Exec.Output(ctx, []string{"image", "ls", "--quiet", "--no-trunc", Image})
	if err != nil {
		return "", r.commandError("image ls", err)
	}
	return strings.TrimPrefix(firstLine(string(out)), "sha256:"), nil
}

func (r Runtime) Pull(ctx context.Context, progress io.Writer) error {
	return r.stream(ctx, progress, "pull", Image)
}

func (r Runtime) Run(ctx context.Context, port int) error {
	return r.run(ctx, "run", "--detach",
		"--name", ContainerName,
		"--label", Label+"=1",
		"--publish", fmt.Sprintf("127.0.0.1:%d:%d", port, containerPort),
		"--volume", VolumeName+":"+dataPath,
		"--env", "ENABLE_TELEMETRY=false",
		"--env", "ENABLE_EXPLORER=false",
		Image)
}

func (r Runtime) Start(ctx context.Context) error { return r.run(ctx, "start", ContainerName) }

func (r Runtime) Stop(ctx context.Context) error { return r.run(ctx, "stop", ContainerName) }

// Remove deletes the container, running or not. Its volume stays.
func (r Runtime) Remove(ctx context.Context) error {
	return r.run(ctx, "rm", "--force", ContainerName)
}

func (r Runtime) HasVolume(ctx context.Context) (bool, error) {
	out, err := r.Exec.Output(ctx, []string{"volume", "ls", "--quiet"})
	if err != nil {
		return false, r.commandError("volume ls", err)
	}
	return slices.Contains(strings.Fields(string(out)), VolumeName), nil
}

func (r Runtime) RemoveVolume(ctx context.Context) error {
	return r.run(ctx, "volume", "rm", VolumeName)
}

func (r Runtime) RemoveImage(ctx context.Context) error {
	return r.run(ctx, "image", "rm", Image)
}

type LogOptions struct {
	Follow bool
	Tail   int
}

func (r Runtime) Logs(ctx context.Context, o LogOptions, w io.Writer) error {
	args := []string{"logs"}
	if o.Follow {
		args = append(args, "--follow")
	}
	args = append(args, "--tail", strconv.Itoa(o.Tail), ContainerName)
	return r.stream(ctx, w, args...)
}

// StatusLine is the emulator's latest status line, as ParseStatusLine reads
// it from the log's last lines.
func (r Runtime) StatusLine(ctx context.Context) (string, bool) {
	var log bytes.Buffer
	if err := r.Exec.Stream(ctx, []string{"logs", "--tail", statusLogLines, ContainerName}, &log); err != nil {
		return "", false
	}
	return ParseStatusLine(log.String())
}

// ParseStatusLine finds the last line of log holding the image's
// "PostgreSQL=OK, Gateway=FAIL, Explorer=OK" status, and returns its
// PostgreSQL and Gateway fields. The wording is the image's, so it only
// ever feeds progress, never a decision.
func ParseStatusLine(log string) (string, bool) {
	lines := strings.Split(log, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		at := strings.Index(lines[i], "PostgreSQL=")
		if at < 0 {
			continue
		}
		var kept []string
		for _, field := range strings.Split(lines[i][at:], ",") {
			field = strings.TrimSpace(field)
			if strings.HasPrefix(field, "PostgreSQL=") || strings.HasPrefix(field, "Gateway=") {
				kept = append(kept, field)
			}
		}
		return strings.Join(kept, ", "), true
	}
	return "", false
}

func (r Runtime) run(ctx context.Context, args ...string) error {
	if _, err := r.Exec.Output(ctx, args); err != nil {
		return r.commandError(verb(args), err)
	}
	return nil
}

func (r Runtime) stream(ctx context.Context, w io.Writer, args ...string) error {
	if err := r.Exec.Stream(ctx, args, w); err != nil {
		return r.commandError(verb(args), err)
	}
	return nil
}

func (r Runtime) commandError(verb string, err error) error {
	return fmt.Errorf("emulator: %s %s: %w", r.Name, verb, err)
}

func verb(args []string) string {
	if len(args) > 1 && (args[0] == "volume" || args[0] == "image") {
		return args[0] + " " + args[1]
	}
	return args[0]
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

// CommandExec runs the binary at Path.
type CommandExec struct {
	Path string
}

func (e CommandExec) Output(ctx context.Context, args []string) ([]byte, error) {
	var stderr bytes.Buffer
	// #nosec G204 -- Path is docker or podman as found on PATH, and every
	// argv is built in this package from constants and a port number.
	cmd := exec.CommandContext(ctx, e.Path, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, &CommandError{Stderr: stderr.String(), Err: err}
	}
	return out, nil
}

func (e CommandExec) Stream(ctx context.Context, args []string, w io.Writer) error {
	// #nosec G204 -- as in Output.
	cmd := exec.CommandContext(ctx, e.Path, args...)
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Run(); err != nil {
		return &CommandError{Err: err}
	}
	return nil
}
