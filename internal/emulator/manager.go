package emulator

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// Probe makes one attempt at a request that only a ready emulator answers.
type Probe func(ctx context.Context) error

// Manager drives the emulator's container. Progress receives what the user
// watches while it works: pull output and the waiting line.
type Manager struct {
	Runtime  Runtime
	Probe    Probe
	Progress io.Writer

	PollInterval     time.Duration
	ProgressInterval time.Duration
	// AttemptTimeout bounds one probe: azcore retries a 503 inside it.
	AttemptTimeout time.Duration
}

func NewManager(runtime Runtime, probe Probe, progress io.Writer) Manager {
	return Manager{
		Runtime:          runtime,
		Probe:            probe,
		Progress:         progress,
		PollInterval:     2 * time.Second,
		ProgressInterval: 10 * time.Second,
		AttemptTimeout:   10 * time.Second,
	}
}

// StartOptions are start's flags. A zero Port keeps the port the container
// publishes, or DefaultPort for a new one.
type StartOptions struct {
	Port     int
	Recreate bool
	Pull     bool
}

// Start brings the container to running, creating it when it is absent, and
// returns the host port it publishes. It does not wait for the emulator to
// answer: that is Wait.
func (m Manager) Start(ctx context.Context, o StartOptions) (int, error) {
	c, err := m.Runtime.Inspect(ctx)
	if err != nil {
		return 0, err
	}
	if err := m.checkReusable(c, o); err != nil {
		return 0, err
	}
	port := o.Port
	if port == 0 {
		port = c.HostPort
	}
	if port == 0 {
		port = DefaultPort
	}
	recreate := o.Recreate
	if o.Pull {
		changed, err := m.pullChanged(ctx, c)
		if err != nil {
			return 0, err
		}
		recreate = recreate || changed
	}
	if recreate && c.Exists() {
		if err := m.Runtime.Remove(ctx); err != nil {
			return 0, err
		}
		c = Container{State: StateAbsent}
	}
	switch {
	case !c.Exists():
		return port, m.create(ctx, port)
	case c.Running():
		return port, nil
	}
	return port, m.Runtime.Start(ctx)
}

// checkReusable refuses, before anything changes, a container Alchemist did
// not create, and one on another port unless it is to be recreated.
func (m Manager) checkReusable(c Container, o StartOptions) error {
	if !c.Exists() {
		return nil
	}
	if !c.Managed {
		return m.notManaged()
	}
	if o.Port != 0 && o.Port != c.HostPort && !o.Recreate {
		return fmt.Errorf("emulator: the container publishes port %d: pass --recreate to move it to %d", c.HostPort, o.Port)
	}
	return nil
}

func (m Manager) notManaged() error {
	return fmt.Errorf("emulator: a container named %s %w: remove it with %s rm -f %s, then run start again",
		ContainerName, ErrNotManaged, m.Runtime.Name, ContainerName)
}

// pullChanged pulls Image and reports whether c runs an older one.
func (m Manager) pullChanged(ctx context.Context, c Container) (bool, error) {
	if err := m.Runtime.Pull(ctx, m.Progress); err != nil {
		return false, err
	}
	pulled, err := m.Runtime.ImageID(ctx)
	if err != nil {
		return false, err
	}
	return c.Exists() && c.ImageID != pulled, nil
}

func (m Manager) create(ctx context.Context, port int) error {
	image, err := m.Runtime.ImageID(ctx)
	if err != nil {
		return err
	}
	if err := checkPortFree(port); err != nil {
		return err
	}
	if image == "" {
		if err := m.Runtime.Pull(ctx, m.Progress); err != nil {
			return err
		}
	}
	return m.Runtime.Run(ctx, port)
}

// checkPortFree turns the runtime's opaque "address already in use" into the
// fix, by trying the port first.
func checkPortFree(port int) error {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("emulator: port %d %w: pass --port to use another%s", port, ErrPortInUse, windowsEmulatorNote())
	}
	return listener.Close()
}

// Stop stops a running container and returns it as it was found. A missing
// or stopped one is not an error: there is nothing to stop.
func (m Manager) Stop(ctx context.Context) (Container, error) {
	c, err := m.Runtime.Inspect(ctx)
	if err != nil || !c.Exists() {
		return c, err
	}
	if !c.Managed {
		return c, m.notManaged()
	}
	if !c.Running() {
		return c, nil
	}
	return c, m.Runtime.Stop(ctx)
}

type RemoveOptions struct {
	Data  bool
	Image bool
}

// Removed is what Remove deleted.
type Removed struct {
	Container, Volume, Image bool
}

// Remove deletes the container, and the data volume and the image when o
// asks. It refuses, before deleting anything, a container it did not create.
func (m Manager) Remove(ctx context.Context, o RemoveOptions) (Removed, error) {
	var removed Removed
	c, err := m.Runtime.Inspect(ctx)
	if err != nil {
		return removed, err
	}
	if c.Exists() && !c.Managed {
		return removed, m.notManaged()
	}
	if c.Exists() {
		if err := m.Runtime.Remove(ctx); err != nil {
			return removed, err
		}
		removed.Container = true
	}
	if o.Data {
		if removed.Volume, err = m.removeVolume(ctx); err != nil {
			return removed, err
		}
	}
	if o.Image {
		removed.Image, err = m.removeImage(ctx)
	}
	return removed, err
}

func (m Manager) removeVolume(ctx context.Context) (bool, error) {
	exists, err := m.Runtime.HasVolume(ctx)
	if err != nil || !exists {
		return false, err
	}
	return true, m.Runtime.RemoveVolume(ctx)
}

func (m Manager) removeImage(ctx context.Context) (bool, error) {
	id, err := m.Runtime.ImageID(ctx)
	if err != nil || id == "" {
		return false, err
	}
	return true, m.Runtime.RemoveImage(ctx)
}

// Logs prints the container's log to w.
func (m Manager) Logs(ctx context.Context, o LogOptions, w io.Writer) error {
	c, err := m.Runtime.Inspect(ctx)
	if err != nil {
		return err
	}
	if !c.Exists() {
		return fmt.Errorf("emulator: %w: run alchemist emulator start", ErrNoContainer)
	}
	return m.Runtime.Logs(ctx, o, w)
}
