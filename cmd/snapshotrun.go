package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/export"
	"github.com/colbytimm/alchemist/internal/snapshot"
	"github.com/colbytimm/alchemist/internal/tui"
	"github.com/colbytimm/alchemist/internal/tui/panes"
)

// liveSnapshot is a connection and what a capture needs from its profile.
type liveSnapshot struct {
	conn     adapter.Connection
	maxItems int64
}

// connectProfile connects the profile called name, as a session would.
func connectProfile(ctx context.Context, keyring config.Keyring, name string) (liveSnapshot, error) {
	store, cfg, err := loadConfig()
	if err != nil {
		return liveSnapshot{}, err
	}
	profile, err := cfg.Profile(name)
	if err != nil {
		return liveSnapshot{}, err
	}
	conn, err := Profiles{Store: store, Keyring: keyring}.Open(ctx, profile.Name)
	if errors.Is(err, tui.ErrCredentialsNeeded) {
		return liveSnapshot{}, fmt.Errorf("cmd: profile %s has no key: set $%s, or run alchemist profile set-key %s: %w",
			name, config.EnvKeyVar(name), name, err)
	}
	if err != nil {
		return liveSnapshot{}, err
	}
	return liveSnapshot{conn: conn, maxItems: profile.SnapshotMaxItems}, nil
}

// close ends the connection once the snapshot is written or has failed;
// a close failure then changes nothing.
func (l liveSnapshot) close() {
	_ = l.conn.Close()
}

// sources is what a capture reads for each container loc names, found in
// the catalog: every container of the database, or the one named.
func (l liveSnapshot) sources(ctx context.Context, loc snapshot.Location) ([]snapshot.Source, error) {
	scanner, ok := l.conn.(adapter.ItemScanner)
	if !ok {
		return nil, fmt.Errorf("cmd: %s: this adapter cannot read every item of a container: %w", loc.Account, adapter.ErrUnsupported)
	}
	definitions, _ := l.conn.(adapter.DefinitionReader)
	throughput, _ := l.conn.(adapter.ThroughputEditor)
	database := adapter.Node{Kind: adapter.NodeDatabase, Name: loc.Database, Path: []string{loc.Database}}
	nodes, err := l.conn.Catalog().Children(ctx, database)
	if err != nil {
		return nil, err
	}
	var sources []snapshot.Source
	for _, node := range nodes {
		if node.Kind != adapter.NodeContainer || loc.Container != "" && node.Name != loc.Container {
			continue
		}
		sources = append(sources, snapshot.Source{
			Container:     node.Path,
			PartitionKeys: strings.Split(node.Meta[adapter.MetaPartitionKey], adapter.PartitionKeyPathSeparator),
			Items:         scanner,
			Definitions:   definitions,
			Throughput:    throughput,
		})
	}
	if loc.Container != "" && len(sources) == 0 {
		return nil, fmt.Errorf("cmd: %s has no container %s", loc.Database, loc.Container)
	}
	return sources, nil
}

type takeFlags struct {
	scope scopeFlags
	note  string
	full  bool
}

func (f takeFlags) run(cmd *cobra.Command, keyring config.Keyring, args []string) error {
	loc, rest, err := f.scope.location(cmd, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("cmd: unexpected argument %q", rest[0])
	}
	live, err := connectProfile(cmd.Context(), keyring, loc.Account)
	if err != nil {
		return err
	}
	defer live.close()
	options := snapshot.CaptureOptions{Note: f.note, Full: f.full, MaxItems: live.maxItems}
	if loc.Container != "" {
		_, err := live.takeContainer(cmd, loc, options)
		return err
	}
	return live.takeDatabase(cmd, loc, options)
}

func (l liveSnapshot) takeContainer(cmd *cobra.Command, loc snapshot.Location, options snapshot.CaptureOptions) (snapshot.Record, error) {
	sources, err := l.sources(cmd.Context(), loc)
	if err != nil {
		return snapshot.Record{}, err
	}
	store, err := snapshot.Open(loc)
	if err != nil {
		return snapshot.Record{}, err
	}
	capture, err := store.Begin(sources[0], options)
	if err != nil {
		return snapshot.Record{}, err
	}
	progress := newProgressLine(cmd)
	last, err := drive(cmd.Context(), capture.Next,
		func(p snapshot.Progress) { progress.show(loc, p) },
		func(p snapshot.Progress) bool { return p.Done })
	progress.end()
	if err != nil {
		return snapshot.Record{}, errors.Join(err, capture.Abort())
	}
	return last.Record, say(cmd, "%s", summaryLine(loc, last.Record))
}

func (l liveSnapshot) takeDatabase(cmd *cobra.Command, loc snapshot.Location, options snapshot.CaptureOptions) error {
	sources, err := l.sources(cmd.Context(), loc)
	if err != nil {
		return err
	}
	throughput, _ := l.conn.(adapter.ThroughputEditor)
	capture, err := snapshot.BeginGroup(loc, snapshot.GroupSource{Containers: sources, Throughput: throughput}, options)
	if err != nil {
		return err
	}
	progress := newProgressLine(cmd)
	last, err := drive(cmd.Context(), capture.Next,
		func(p snapshot.GroupProgress) {
			container := loc
			container.Container = p.Container
			progress.show(container, p.Capture)
		},
		func(p snapshot.GroupProgress) bool { return p.Done })
	progress.end()
	if err != nil {
		return errors.Join(err, capture.Abort())
	}
	return sayGroup(cmd, loc, last.Group)
}

func sayGroup(cmd *cobra.Command, loc snapshot.Location, group snapshot.Group) error {
	failed := 0
	for _, member := range group.Containers {
		container := loc
		container.Container = member.Container
		line := fmt.Sprintf("%s: failed: %s", container, member.Error)
		if member.Error == "" {
			record, err := readRecord(container, member.Snapshot)
			if err != nil {
				return err
			}
			line = summaryLine(container, record)
		} else {
			failed++
		}
		if err := say(cmd, "%s", line); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("cmd: %d of %d containers of %s failed", failed, len(group.Containers), loc)
	}
	return nil
}

func readRecord(loc snapshot.Location, id string) (snapshot.Record, error) {
	store, err := snapshot.Open(loc)
	if err != nil {
		return snapshot.Record{}, err
	}
	return store.Resolve(id)
}

// drive calls next until done says so, showing each step to report and
// waiting out up to snapshot.MaxThrottles throttled steps in a row.
func drive[P any](ctx context.Context, next func(context.Context) (P, error), report func(P), done func(P) bool) (P, error) {
	throttles := 0
	for {
		progress, err := next(ctx)
		var throttled *adapter.ThrottledError
		if errors.As(err, &throttled) && throttles < snapshot.MaxThrottles {
			throttles++
			if err := wait(ctx, throttled.Wait()); err != nil {
				return progress, err
			}
			continue
		}
		if err != nil {
			return progress, err
		}
		throttles = 0
		report(progress)
		if done(progress) {
			return progress, nil
		}
	}
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// summaryLine is the one line take prints for a snapshot.
func summaryLine(loc snapshot.Location, r snapshot.Record) string {
	return fmt.Sprintf("%s.%s %s: %s items, %s, %s RU, %s new", loc.Database, loc.Container, r.ID,
		countText(r.Items), changesText(r), panes.FormatCharge(r.RequestCharge), panes.FormatBytes(r.StoredBytes))
}

func changesText(r snapshot.Record) string {
	if r.Parent == "" {
		return "first snapshot"
	}
	return fmt.Sprintf("+%s −%s ~%s", countText(r.Added), countText(r.Removed), countText(r.Modified))
}

func countText(n int64) string { return panes.FormatCount(n) }

// progressLine rewrites one line of stderr while a capture runs, and only
// when stderr is a terminal: cron gets the summary line alone.
type progressLine struct {
	out   io.Writer
	shown bool
}

func newProgressLine(cmd *cobra.Command) *progressLine {
	file, ok := cmd.ErrOrStderr().(*os.File)
	if !ok || !term.IsTerminal(file.Fd()) {
		return &progressLine{}
	}
	return &progressLine{out: file}
}

func (p *progressLine) show(loc snapshot.Location, progress snapshot.Progress) {
	if p.out == nil {
		return
	}
	p.shown = true
	_, _ = fmt.Fprintf(p.out, "\r\x1b[K%s %s: %s items · %s RU · %s read · %s new", // a lost progress line is only cosmetic
		loc, progress.Phase, countText(progress.Items), panes.FormatCharge(progress.RequestCharge),
		panes.FormatBytes(progress.BytesRead), panes.FormatBytes(progress.BytesStored))
}

func (p *progressLine) end() {
	if p.shown {
		_, _ = fmt.Fprint(p.out, "\r\x1b[K") // as above
	}
}

type diffFlags struct {
	scope  scopeFlags
	live   bool
	output string
}

func (f diffFlags) run(cmd *cobra.Command, keyring config.Keyring, args []string) error {
	loc, rest, err := f.scope.containerLocation(cmd, args)
	if err != nil {
		return err
	}
	from, to, err := f.ends(cmd, keyring, loc, rest)
	if err != nil {
		return err
	}
	store, err := snapshot.Open(loc)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }() // read-only: a close failure loses nothing
	from, to, err = resolvePair(store, from, to)
	if err != nil {
		return err
	}
	d, err := store.Diff(from, to)
	if err != nil {
		return err
	}
	if f.output != "" {
		path, err := export.ResolvePath(f.output)
		if err != nil {
			return err
		}
		if err := store.WriteDiff(path, d, snapshot.RefuseExisting); err != nil {
			return err
		}
	}
	return writeDiff(cmd.OutOrStdout(), store, loc, d)
}

// ends are the two snapshots to compare, by reference. A live diff takes
// its to first, and compares it with from, the newest before it by
// default.
func (f diffFlags) ends(cmd *cobra.Command, keyring config.Keyring, loc snapshot.Location, rest []string) (string, string, error) {
	if !f.live {
		if len(rest) > 2 {
			return "", "", fmt.Errorf("cmd: unexpected argument %q", rest[2])
		}
		return argOr(rest, 0, snapshot.RefPrevious), argOr(rest, 1, snapshot.RefLatest), nil
	}
	if len(rest) > 1 {
		return "", "", errors.New("cmd: --live compares with one snapshot, and takes the other")
	}
	store, err := snapshot.Open(loc)
	if err != nil {
		return "", "", err
	}
	from, err := store.Resolve(argOr(rest, 0, snapshot.RefLatest))
	if err != nil {
		return "", "", err
	}
	live, err := connectProfile(cmd.Context(), keyring, loc.Account)
	if err != nil {
		return "", "", err
	}
	defer live.close()
	record, err := live.takeContainer(cmd, loc, snapshot.CaptureOptions{MaxItems: live.maxItems})
	return from.ID, record.ID, err
}

func resolvePair(store *snapshot.Store, from, to string) (string, string, error) {
	first, err := store.Resolve(from)
	if err != nil {
		return "", "", err
	}
	second, err := store.Resolve(to)
	if err != nil {
		return "", "", err
	}
	return first.ID, second.ID, nil
}

var changeSigns = map[snapshot.ChangeKind]string{snapshot.Added: "+", snapshot.Removed: "−", snapshot.Modified: "~"}

func writeDiff(w io.Writer, store *snapshot.Store, loc snapshot.Location, d snapshot.Diff) error {
	var out strings.Builder
	fmt.Fprintf(&out, "%s %s → %s: +%d −%d ~%d\n%s\n", loc, d.From.ID, d.To.ID,
		d.Count(snapshot.Added), d.Count(snapshot.Removed), d.Count(snapshot.Modified), d.Definition.Summary())
	for _, change := range d.Definition.Changes {
		fmt.Fprintf(&out, "  %s\n", change)
	}
	for _, id := range d.MovedIDs() {
		fmt.Fprintf(&out, "%s changed partition key: in Cosmos that is a removal and an addition\n", id)
	}
	var rows strings.Builder
	for _, item := range d.Items {
		fields, err := store.ChangedFields(item)
		if err != nil {
			return err
		}
		fmt.Fprintf(&rows, "%s\t%s\t%s\t%s\n", changeSigns[item.Kind], item.Identity.PartitionKeyText(), item.Identity.ID, strings.Join(fields, ", "))
	}
	if err := writeTable(&out, rows.String()); err != nil {
		return fmt.Errorf("cmd: write diff: %w", err)
	}
	if _, err := io.WriteString(w, out.String()); err != nil {
		return fmt.Errorf("cmd: write diff: %w", err)
	}
	return nil
}

type listFlags struct {
	scope scopeFlags
	json  bool
}

type listedStore struct {
	Account   string            `json:"account"`
	Database  string            `json:"database"`
	Container string            `json:"container"`
	Profile   bool              `json:"profile"`
	Usage     snapshot.Usage    `json:"usage"`
	Snapshots []snapshot.Record `json:"snapshots"`
	store     *snapshot.Store
}

func (f *listFlags) run(cmd *cobra.Command, args []string) error {
	root, err := snapshotRoot(cmd)
	if err != nil {
		return err
	}
	all, err := snapshot.Stores(root)
	if err != nil {
		return err
	}
	stores, err := f.listed(args, all)
	if err != nil {
		return err
	}
	if f.json {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(stores)
	}
	if len(stores) == 0 {
		return say(cmd, "no snapshots under %s", root)
	}
	return writeStores(cmd.OutOrStdout(), stores)
}

func (f *listFlags) listed(args []string, all []snapshot.Location) ([]listedStore, error) {
	var want snapshot.Location
	if len(args) > 0 {
		want.Account = args[0]
	}
	if len(args) > 1 || f.scope.database != "" || f.scope.container != "" {
		scope, rest, err := f.scope.scopeArgs(args[1:])
		if err != nil {
			return nil, err
		}
		if len(rest) > 0 {
			return nil, fmt.Errorf("cmd: unexpected argument %q", rest[0])
		}
		want.Database, want.Container = scope.Database, scope.Container
	}
	_, cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	var stores []listedStore
	for _, loc := range all {
		if !matches(want, loc) {
			continue
		}
		listed, err := listStore(loc, cfg)
		if err != nil {
			return nil, err
		}
		stores = append(stores, listed)
	}
	return stores, nil
}

func matches(want, loc snapshot.Location) bool {
	return (want.Account == "" || want.Account == loc.Account) &&
		(want.Database == "" || want.Database == loc.Database) &&
		(want.Container == "" || want.Container == loc.Container)
}

func listStore(loc snapshot.Location, cfg config.Config) (listedStore, error) {
	store, err := snapshot.Open(loc)
	if err != nil {
		return listedStore{}, err
	}
	usage, err := store.Usage()
	if err != nil {
		return listedStore{}, err
	}
	_, known := cfg.Profiles[loc.Account]
	return listedStore{
		Account: loc.Account, Database: loc.Database, Container: loc.Container, Profile: known,
		Usage: usage, Snapshots: store.Snapshots(), store: store,
	}, nil
}

func writeStores(w io.Writer, stores []listedStore) error {
	var out strings.Builder
	for i, listed := range stores {
		if i > 0 {
			out.WriteString("\n")
		}
		loc := snapshot.Location{Account: listed.Account, Database: listed.Database, Container: listed.Container}
		orphan := ""
		if !listed.Profile {
			orphan = " (no profile)"
		}
		fmt.Fprintf(&out, "%s%s: %s\n", loc, orphan, panes.FormatUsage(listed.Usage))
		var rows strings.Builder
		rows.WriteString("ID\tTAKEN (UTC)\tWINDOW\tITEMS\tCHANGES\tNEW DATA\tNOTE\n")
		for _, record := range slices.Backward(listed.Snapshots) {
			fmt.Fprintf(&rows, "%s\t%s\t%s\t%s\t%s%s\t%s\t%s\n", record.ID, record.Time().Format("2006-01-02 15:04"),
				record.Window().Round(time.Second), countText(record.Items), changesText(record), definitionMark(listed.store, record),
				panes.FormatBytes(record.StoredBytes), record.Note)
		}
		if err := writeTable(&out, rows.String()); err != nil {
			return fmt.Errorf("cmd: write snapshots: %w", err)
		}
	}
	_, err := io.WriteString(w, out.String())
	return err
}

// writeTable aligns tab-separated rows into columns.
func writeTable(w io.Writer, rows string) error {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := io.WriteString(table, rows); err != nil {
		return err
	}
	return table.Flush()
}

func definitionMark(store *snapshot.Store, record snapshot.Record) string {
	if store.DefinitionChanged(record) {
		return " def"
	}
	return ""
}
