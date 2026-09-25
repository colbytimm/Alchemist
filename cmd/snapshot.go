package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/colbytimm/alchemist/internal/config"
	"github.com/colbytimm/alchemist/internal/export"
	"github.com/colbytimm/alchemist/internal/snapshot"
)

const snapshotDirFlag = "snapshot-dir"

const snapshotLong = "A snapshot is a copy of a container's items and definition, kept under\n" +
	"$XDG_DATA_HOME/alchemist/snapshots (~/.local/share/alchemist/snapshots), or snapshot_dir in\n" +
	"config.toml, or --snapshot-dir. A later snapshot stores only what changed, so thirty daily\n" +
	"snapshots cost little more than one. Snapshots are not encrypted: they are copies of your\n" +
	"data protected by file permissions (0600) and whatever encrypts the disk.\n\n" +
	"A snapshot of a live container is per-item versions read during the window its record\n" +
	"shows, not a point in time: there is no consistency between items.\n\n" +
	"Only take and diff --live connect; the rest read the disk, and work for an account whose\n" +
	"profile is gone."

func newSnapshotCmd(keyring config.Keyring) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Take, list, diff and export snapshots of containers",
		Long:  snapshotLong,
		Example: "  alchemist snapshot take prod sales.orders --note nightly\n" +
			"  alchemist snapshot diff prod sales.orders\n" +
			"  0 6 * * *  alchemist snapshot take prod sales --note nightly \\\n" +
			"             && alchemist snapshot prune prod sales --keep-last 7 --keep-daily 30",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(
		newSnapshotTakeCmd(keyring),
		newSnapshotListCmd(),
		newSnapshotDiffCmd(keyring),
		newSnapshotExportCmd(),
		newSnapshotDeleteCmd(),
		newSnapshotPruneCmd(),
		newSnapshotVerifyCmd(),
	)
	return cmd
}

// snapshotRoot is where snapshots live: --snapshot-dir, then snapshot_dir in
// config.toml, then the data directory.
func snapshotRoot(cmd *cobra.Command) (string, error) {
	if flag := cmd.Flags().Lookup(snapshotDirFlag); flag != nil && flag.Value.String() != "" {
		return export.ResolvePath(flag.Value.String())
	}
	_, cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	if cfg.SnapshotDir != "" {
		return export.ResolvePath(cfg.SnapshotDir)
	}
	return snapshot.DefaultRoot()
}

// scopeFlags name a database or a container whose name holds a dot, which
// the <db>.<container> argument cannot.
type scopeFlags struct {
	database  string
	container string
}

func (s *scopeFlags) bind(flags *pflag.FlagSet) {
	flags.StringVar(&s.database, "database", "", "the database, for a name holding a dot")
	flags.StringVar(&s.container, "container", "", "the container, for a name holding a dot")
}

// scopeArgs splits what follows the profile into the scope and the rest:
// the scope is the first of them unless the flags name it.
func (s scopeFlags) scopeArgs(args []string) (snapshot.Location, []string, error) {
	if s.database != "" {
		return snapshot.Location{Database: s.database, Container: s.container}, args, nil
	}
	if s.container != "" {
		return snapshot.Location{}, nil, errors.New("cmd: --container needs --database")
	}
	if len(args) == 0 {
		return snapshot.Location{}, nil, errors.New("cmd: name a database, or a database and container as <db>.<container>")
	}
	database, container, _ := strings.Cut(args[0], ".")
	if database == "" {
		return snapshot.Location{}, nil, fmt.Errorf("cmd: %q names no database", args[0])
	}
	return snapshot.Location{Database: database, Container: container}, args[1:], nil
}

// location is the store the command's profile and scope name.
func (s scopeFlags) location(cmd *cobra.Command, args []string) (snapshot.Location, []string, error) {
	root, err := snapshotRoot(cmd)
	if err != nil {
		return snapshot.Location{}, nil, err
	}
	loc, rest, err := s.scopeArgs(args[1:])
	if err != nil {
		return snapshot.Location{}, nil, err
	}
	loc.Root, loc.Account = root, args[0]
	return loc, rest, nil
}

// containerLocation is location, refusing a scope that names a database
// alone.
func (s scopeFlags) containerLocation(cmd *cobra.Command, args []string) (snapshot.Location, []string, error) {
	loc, rest, err := s.location(cmd, args)
	if err == nil && loc.Container == "" {
		err = fmt.Errorf("cmd: %s names a database: give <db>.<container>", loc.Database)
	}
	return loc, rest, err
}

// storesUnder lists the stores loc names: its container's, or every one of
// its database's.
func storesUnder(loc snapshot.Location) ([]snapshot.Location, error) {
	if loc.Container != "" {
		return []snapshot.Location{loc}, nil
	}
	all, err := snapshot.Stores(loc.Root)
	if err != nil {
		return nil, err
	}
	var matching []snapshot.Location
	for _, candidate := range all {
		if candidate.Account == loc.Account && candidate.Database == loc.Database {
			matching = append(matching, candidate)
		}
	}
	if len(matching) == 0 {
		return nil, fmt.Errorf("cmd: %s: %w", loc, snapshot.ErrNoSnapshot)
	}
	return matching, nil
}

func newSnapshotTakeCmd(keyring config.Keyring) *cobra.Command {
	var take takeFlags
	cmd := &cobra.Command{
		Use:   "take <profile> <db>[.<container>]",
		Short: "Snapshot a container, or every container of a database",
		Long: "take reads the container once for the first snapshot; after that it reads every item's\n" +
			"key and version, and the bodies of those that changed. Snapshots are not encrypted.\n" +
			"A progress line goes to stderr when it is a terminal; one summary line per container\n" +
			"goes to stdout.",
		Example: "  alchemist snapshot take prod sales.orders --note \"before the migration\"\n" +
			"  alchemist snapshot take prod sales",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return take.run(cmd, keyring, args)
		},
	}
	cmd.Flags().StringVar(&take.note, "note", "", "a note kept with the snapshot")
	cmd.Flags().BoolVar(&take.full, "full", false, "read every item, even when a sweep of keys would do")
	take.scope.bind(cmd.Flags())
	return cmd
}

func newSnapshotListCmd() *cobra.Command {
	var list listFlags
	cmd := &cobra.Command{
		Use:   "list [<profile> [<db>[.<container>]]]",
		Short: "List snapshots, and what they cost on disk",
		Long: "list shows every snapshot of each store with its window, items, changes and new data,\n" +
			"and what the store weighs against the exports it replaces. With no arguments it lists\n" +
			"every store on disk, including those of accounts no profile names any more.",
		Args:         cobra.MaximumNArgs(2),
		SilenceUsage: true,
		RunE:         list.run,
	}
	cmd.Flags().BoolVar(&list.json, "json", false, "write the records and usage as JSON")
	list.scope.bind(cmd.Flags())
	return cmd
}

func newSnapshotDiffCmd(keyring config.Keyring) *cobra.Command {
	var diff diffFlags
	cmd := &cobra.Command{
		Use:   "diff <profile> <db>.<container> [<from> [<to>]]",
		Short: "Show what changed between two snapshots",
		Long: "diff compares two snapshots: previous and latest unless named. A snapshot is named by\n" +
			"its id, a unique prefix of one, latest or previous. --live takes a snapshot first and\n" +
			"compares it with <from>, latest by default; the new snapshot is kept.",
		Example: "  alchemist snapshot diff prod sales.orders\n" +
			"  alchemist snapshot diff prod sales.orders 20260918 latest -o changes.csv\n" +
			"  alchemist snapshot diff prod sales.orders --live",
		Args:         cobra.RangeArgs(1, 4),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return diff.run(cmd, keyring, args)
		},
	}
	cmd.Flags().BoolVar(&diff.live, "live", false, "take a snapshot now and diff against it")
	cmd.Flags().StringVarP(&diff.output, "output", "o", "", "write the diff to a .json or .csv file")
	diff.scope.bind(cmd.Flags())
	return cmd
}

func newSnapshotExportCmd() *cobra.Command {
	var scope scopeFlags
	var output string
	cmd := &cobra.Command{
		Use:          "export <profile> <db>.<container> [<snapshot>] -o <file>",
		Short:        "Write a snapshot's items to a .jsonl or .json file",
		Args:         cobra.RangeArgs(1, 3),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return exportSnapshot(cmd, scope, args, output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "the file to write: .jsonl, one item per line, or .json")
	scope.bind(cmd.Flags())
	return cmd
}

func newSnapshotDeleteCmd() *cobra.Command {
	var scope scopeFlags
	cmd := &cobra.Command{
		Use:          "delete <profile> <db>.<container> <snapshot>",
		Short:        "Delete one snapshot, then reclaim what no other needs",
		Args:         cobra.RangeArgs(2, 3),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteSnapshot(cmd, scope, args)
		},
	}
	scope.bind(cmd.Flags())
	return cmd
}

func newSnapshotPruneCmd() *cobra.Command {
	var prune pruneFlags
	cmd := &cobra.Command{
		Use:   "prune <profile> <db>[.<container>] --keep-last N [--keep-daily D]",
		Short: "Delete the snapshots a retention policy does not keep",
		Long: "prune keeps the newest N snapshots, and the newest of each of the last D UTC days;\n" +
			"the union is kept, and the newest snapshot always is. Nothing prunes by itself.",
		Example:      "  alchemist snapshot prune prod sales --keep-last 7 --keep-daily 30 --dry-run",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE:         prune.run,
	}
	cmd.Flags().IntVar(&prune.policy.KeepLast, "keep-last", 0, "keep the newest N snapshots")
	cmd.Flags().IntVar(&prune.policy.KeepDaily, "keep-daily", 0, "keep the newest snapshot of each of the last D UTC days")
	cmd.Flags().BoolVar(&prune.dryRun, "dry-run", false, "list what would be deleted, and delete nothing")
	prune.scope.bind(cmd.Flags())
	return cmd
}

func newSnapshotVerifyCmd() *cobra.Command {
	var verify verifyFlags
	cmd := &cobra.Command{
		Use:   "verify <profile> <db>[.<container>]",
		Short: "Check that every snapshot is whole and readable",
		Long: "verify checks every file's format line, every checksum, that every change set leads to\n" +
			"the snapshot after it, and that every body a snapshot names is stored. --deep also\n" +
			"decompresses and rehashes every body. It fails on any problem.",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE:         verify.run,
	}
	cmd.Flags().BoolVar(&verify.options.Deep, "deep", false, "decompress every block and rehash every body")
	cmd.Flags().BoolVar(&verify.options.RebuildIndex, "rebuild-index", false, "rewrite every pack's index from the pack first")
	verify.scope.bind(cmd.Flags())
	return cmd
}

func exportSnapshot(cmd *cobra.Command, scope scopeFlags, args []string, output string) error {
	if output == "" {
		return errors.New("cmd: name the file to write with -o")
	}
	loc, rest, err := scope.containerLocation(cmd, args)
	if err != nil {
		return err
	}
	store, err := snapshot.Open(loc)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }() // read-only: a close failure loses nothing
	record, err := store.Resolve(argOr(rest, 0, snapshot.RefLatest))
	if err != nil {
		return err
	}
	path, err := export.ResolvePath(output)
	if err != nil {
		return err
	}
	if err := store.WriteItems(path, record.ID, snapshot.RefuseExisting); err != nil {
		return err
	}
	return say(cmd, "wrote %s items of %s %s to %s", countText(record.Items), loc, record.ID, path)
}

func deleteSnapshot(cmd *cobra.Command, scope scopeFlags, args []string) error {
	loc, rest, err := scope.containerLocation(cmd, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("cmd: name the snapshot to delete")
	}
	store, err := snapshot.Open(loc)
	if err != nil {
		return err
	}
	record, err := store.Resolve(rest[0])
	if err != nil {
		return err
	}
	if err := store.Delete(record.ID, time.Now()); err != nil {
		return err
	}
	return say(cmd, "deleted %s %s", loc, record.ID)
}

type pruneFlags struct {
	scope  scopeFlags
	policy snapshot.Policy
	dryRun bool
}

func (p *pruneFlags) run(cmd *cobra.Command, args []string) error {
	if !cmd.Flags().Changed("keep-last") {
		return errors.New("cmd: prune needs --keep-last, which may be 0 to keep by day alone")
	}
	loc, _, err := p.scope.location(cmd, args)
	if err != nil {
		return err
	}
	stores, err := storesUnder(loc)
	if err != nil {
		return err
	}
	for _, loc := range stores {
		if err := p.pruneStore(cmd, loc); err != nil {
			return err
		}
	}
	return nil
}

func (p pruneFlags) pruneStore(cmd *cobra.Command, loc snapshot.Location) error {
	store, err := snapshot.Open(loc)
	if err != nil {
		return err
	}
	now := time.Now()
	pruned := store.Pruned(p.policy, now)
	kept := len(store.Snapshots()) - len(pruned)
	verb := "would delete"
	if !p.dryRun {
		verb = "deleted"
		if pruned, err = store.Prune(p.policy, now); err != nil {
			return err
		}
	}
	for _, record := range pruned {
		if err := say(cmd, "%s %s %s", verb, loc, record.ID); err != nil {
			return err
		}
	}
	return say(cmd, "%s: %d kept, %d %s", loc, kept, len(pruned), verb)
}

type verifyFlags struct {
	scope   scopeFlags
	options snapshot.VerifyOptions
}

func (v *verifyFlags) run(cmd *cobra.Command, args []string) error {
	loc, _, err := v.scope.location(cmd, args)
	if err != nil {
		return err
	}
	stores, err := storesUnder(loc)
	if err != nil {
		return err
	}
	var failed []error
	for _, loc := range stores {
		store, err := snapshot.Open(loc)
		if err != nil {
			failed = append(failed, err)
			continue
		}
		report, err := store.Verify(v.options, time.Now())
		if err != nil {
			failed = append(failed, err)
			continue
		}
		if err := say(cmd, "%s: ok, %d snapshots, %d packs, %s bodies", loc, report.Snapshots, report.Packs, countText(int64(report.Bodies))); err != nil {
			return err
		}
	}
	return errors.Join(failed...)
}

func argOr(args []string, i int, fallback string) string {
	if i < len(args) {
		return args[i]
	}
	return fallback
}
