package snapshot

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
)

// Group is a database snapshot: one container snapshot per container,
// taken one after another, and the database's own definition. A container
// that failed is listed with its error; the ones that succeeded stand.
type Group struct {
	Format     int               `json:"format"`
	ID         string            `json:"id"`
	Database   string            `json:"database"`
	Note       string            `json:"note,omitempty"`
	Started    time.Time         `json:"started"`
	Finished   time.Time         `json:"finished"`
	Throughput *throughputRecord `json:"throughput,omitempty"`
	Containers []GroupMember     `json:"containers"`
}

type GroupMember struct {
	Container string `json:"container"`
	Snapshot  string `json:"snapshot,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Groups lists the database snapshots of loc's database, oldest first.
func Groups(loc Location) ([]Group, error) {
	paths, err := filepath.Glob(filepath.Join(loc.DatabaseDir(), groupsDir, "*"+jsonExt))
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	groups := make([]Group, 0, len(paths))
	for _, path := range paths {
		var group Group
		if err := readJSON(path, &group); err != nil {
			return nil, err
		}
		if group.Format != documentFormat {
			return nil, fmt.Errorf("snapshot: %s: format %d: %w", path, group.Format, ErrUnknownFormat)
		}
		groups = append(groups, group)
	}
	slices.SortFunc(groups, func(a, b Group) int { return strings.Compare(a.ID, b.ID) })
	return groups, nil
}

func groupPath(loc Location, id string) string {
	return filepath.Join(loc.DatabaseDir(), groupsDir, id+jsonExt)
}

// GroupSource is a database to snapshot: each container's source, and
// what reads the database's own throughput, which may be nil.
type GroupSource struct {
	Containers []Source
	Throughput adapter.ThroughputEditor
}

// GroupProgress is how far a database snapshot has got: the container in
// progress, counted from zero, and its capture's progress.
type GroupProgress struct {
	Container string
	Index     int
	Count     int
	Capture   Progress
	Done      bool
	Group     Group
}

// GroupCapture takes a database snapshot a step at a time, as Capture
// takes a container's. Its containers are captured in turn, never at
// once, so one scan's request units are spent at a time.
type GroupCapture struct {
	loc      Location
	source   GroupSource
	options  CaptureOptions
	group    Group
	index    int
	current  *Capture
	progress GroupProgress
}

// BeginGroup prepares a database snapshot of loc's database. Its id is its
// start, or a second past the newest group's when that would repeat it.
func BeginGroup(loc Location, source GroupSource, options CaptureOptions) (*GroupCapture, error) {
	if options.Clock == nil {
		options.Clock = time.Now
	}
	existing, err := Groups(loc)
	if err != nil {
		return nil, err
	}
	var newest string
	if len(existing) > 0 {
		newest = existing[len(existing)-1].ID
	}
	started := options.Clock().UTC()
	group := Group{Format: documentFormat, ID: nextID(started, newest), Database: loc.Database, Note: options.Note, Started: started}
	options.Group = group.ID
	return &GroupCapture{loc: loc, source: source, options: options, group: group, progress: GroupProgress{Count: len(source.Containers)}}, nil
}

func (g *GroupCapture) ID() string { return g.group.ID }

// Next takes one step of the container in progress. A container that fails
// for any reason but a throttle or a cancel is recorded as failed and the
// next one begins; those two are returned, to wait and call again or to
// Abort.
func (g *GroupCapture) Next(ctx context.Context) (GroupProgress, error) {
	if g.index == len(g.source.Containers) {
		return g.finish(ctx)
	}
	source := g.source.Containers[g.index]
	name := source.Container[len(source.Container)-1]
	g.progress.Container, g.progress.Index = name, g.index
	if g.current == nil {
		capture, err := g.begin(name, source)
		if err != nil {
			return g.fail(name, err), nil
		}
		g.current = capture
	}
	progress, err := g.current.Next(ctx)
	g.progress.Capture = progress
	var throttled *adapter.ThrottledError
	switch {
	case errors.As(err, &throttled) || errors.Is(err, context.Canceled):
		return g.progress, err
	case err != nil:
		return g.fail(name, errors.Join(err, g.current.Abort())), nil
	case progress.Done:
		g.group.Containers = append(g.group.Containers, GroupMember{Container: name, Snapshot: progress.Record.ID})
		g.current = nil
		g.index++
	}
	return g.progress, nil
}

func (g *GroupCapture) begin(name string, source Source) (*Capture, error) {
	loc := g.loc
	loc.Container = name
	store, err := Open(loc)
	if err != nil {
		return nil, err
	}
	return store.Begin(source, g.options)
}

func (g *GroupCapture) fail(name string, err error) GroupProgress {
	g.group.Containers = append(g.group.Containers, GroupMember{Container: name, Error: err.Error()})
	g.current = nil
	g.index++
	return g.progress
}

// finish reads the database's throughput and publishes the group.
func (g *GroupCapture) finish(ctx context.Context) (GroupProgress, error) {
	if g.source.Throughput != nil {
		throughput, err := g.source.Throughput.Throughput(ctx, []string{g.loc.Database})
		if err != nil {
			return g.progress, err
		}
		g.group.Throughput = newThroughputRecord(throughput)
	}
	g.group.Finished = g.options.Clock().UTC()
	if err := ensureDir(filepath.Join(g.loc.DatabaseDir(), groupsDir)); err != nil {
		return g.progress, err
	}
	if err := writeJSON(groupPath(g.loc, g.group.ID), g.group); err != nil {
		return g.progress, err
	}
	g.progress.Done, g.progress.Group = true, g.group
	return g.progress, nil
}

// Abort stops the container in progress. The containers already captured
// stand as snapshots of their own; the group is never published.
func (g *GroupCapture) Abort() error {
	if g.current == nil {
		return nil
	}
	err := g.current.Abort()
	g.current = nil
	return err
}

type GroupChange struct {
	Container string
	State     GroupChangeState
	From      string
	To        string
	Added     int
	Removed   int
	Modified  int
	Err       error
}

type GroupChangeState int

const (
	ContainerCompared GroupChangeState = iota
	ContainerAppeared
	ContainerDisappeared
	// ContainerNotCompared is a container one of the two groups failed to
	// capture.
	ContainerNotCompared
)

func DiffGroups(loc Location, from, to Group) ([]GroupChange, error) {
	before, after := members(from), members(to)
	names := make([]string, 0, len(before)+len(after))
	for name := range before {
		names = append(names, name)
	}
	for name := range after {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	changes := make([]GroupChange, 0, len(names))
	for _, name := range names {
		change, err := diffMember(loc, name, before[name], after[name])
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func members(g Group) map[string]GroupMember {
	byName := map[string]GroupMember{}
	for _, member := range g.Containers {
		byName[member.Container] = member
	}
	return byName
}

func diffMember(loc Location, name string, before, after GroupMember) (GroupChange, error) {
	change := GroupChange{Container: name, From: before.Snapshot, To: after.Snapshot}
	switch {
	case before.Container == "":
		change.State = ContainerAppeared
		return change, nil
	case after.Container == "":
		change.State = ContainerDisappeared
		return change, nil
	case before.Snapshot == "" || after.Snapshot == "":
		change.State = ContainerNotCompared
		return change, nil
	}
	loc.Container = name
	store, err := Open(loc)
	if err != nil {
		return GroupChange{}, err
	}
	defer func() { _ = store.Close() }() // read-only: a close failure loses nothing
	d, err := store.Diff(before.Snapshot, after.Snapshot)
	if errors.Is(err, ErrNoSnapshot) {
		change.State, change.Err = ContainerNotCompared, err
		return change, nil
	}
	if err != nil {
		return GroupChange{}, err
	}
	change.Added, change.Removed, change.Modified = d.Count(Added), d.Count(Removed), d.Count(Modified)
	return change, nil
}
