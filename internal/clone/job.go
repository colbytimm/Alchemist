// Package clone copies a container, or a database and every container in
// it, from one connection to another, client-side: read the definition,
// create the target, read every item, write every item. Its unit is one
// step, so a caller can run a job as a chain of short calls and stay
// responsive between them. It knows the adapter interfaces and nothing else
// of the backends.
package clone

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/writers"
)

const (
	MinimumRUs = 400
	// MaxSkipped is how many items one container's copy may skip before it
	// stops: past that, something systematic is wrong.
	MaxSkipped = 100
)

var (
	// ErrTargetExists refuses a target that is already there. A clone never
	// writes into anything that existed before it.
	ErrTargetExists   = errors.New("the target already exists, and a clone never writes into it")
	ErrTooManySkipped = fmt.Errorf("more than %d items could not be written", MaxSkipped)
	errNoItemAccess   = errors.New("items cannot be copied between these accounts: definition only")
	errNoThroughput   = errors.New("the source's throughput cannot be read, so it cannot be copied")
)

// Endpoint names one end of a job: [database] or [database, container] on
// an account.
type Endpoint struct {
	Account string // for messages and logs only
	Path    []string
}

func (e Endpoint) String() string {
	return e.Account + "/" + strings.Join(e.Path, ".")
}

func (e Endpoint) database() string { return e.Path[0] }

func (e Endpoint) Container() bool { return len(e.Path) == 2 }

type Content int

const (
	DefinitionAndItems Content = iota
	DefinitionOnly
)

func (c Content) String() string {
	if c == DefinitionOnly {
		return "definition only"
	}
	return "definition and items"
}

// Capacity is what the target is provisioned with, wherever the source had
// capacity of its own. A container drawing on its database's keeps doing so
// wherever it can.
type Capacity int

const (
	Minimum Capacity = iota
	SameAsSource
	None
)

func (c Capacity) String() string {
	switch c {
	case SameAsSource:
		return "same as source"
	case None:
		return "none"
	}
	return fmt.Sprintf("minimum (%d RU/s, manual)", MinimumRUs)
}

type Job struct {
	Source, Target Endpoint
	Content        Content
	Fidelity       adapter.DefinitionFidelity
	Capacity       Capacity
	Writers        int
	// Clock is what a throttled copy waits on; nil waits in real time.
	Clock writers.Clock
}

func (j Job) clock() writers.Clock {
	if j.Clock == nil {
		return writers.SystemClock{}
	}
	return j.Clock
}

// Source and Target are the capabilities a job needs, built by the caller
// from whatever its two connections satisfy.
type Source struct {
	Catalog     adapter.Catalog
	Definitions adapter.DefinitionReader
	Throughput  adapter.ThroughputEditor // nil: the backend has no such concept
	Items       adapter.ItemScanner      // nil allowed for DefinitionOnly
}

type Target struct {
	Catalog adapter.Catalog
	Admin   adapter.CatalogAdmin
	Items   adapter.ItemWriter // nil allowed for DefinitionOnly
}

// Survey is what the source holds, read before anything about the target is
// known: every container a job would copy, in catalog order, and the
// source's own throughput.
type Survey struct {
	Source Endpoint
	// Throughput is the source database's for a database clone, the
	// container's otherwise. Known is false when the source cannot say.
	Throughput      adapter.Throughput
	ThroughputKnown bool
	Containers      []SourceContainer
}

type SourceContainer struct {
	Path            []string
	Definition      adapter.ContainerDefinition
	Throughput      adapter.Throughput
	ThroughputKnown bool
}

// Size is what every container of the survey holds together; it is known
// only when every container's is.
func (s Survey) Size() adapter.SizeEstimate {
	return totalSize(s.Containers, func(c SourceContainer) adapter.SizeEstimate { return c.Definition.Size })
}

func totalSize[T any](items []T, size func(T) adapter.SizeEstimate) adapter.SizeEstimate {
	total := adapter.SizeEstimate{Known: true}
	for _, item := range items {
		s := size(item)
		total.Items += s.Items
		total.Bytes += s.Bytes
		total.Known = total.Known && s.Known
	}
	return total
}

// SurveySource reads what the source at endpoint holds. It reads
// definitions and throughput, and never an item.
func SurveySource(ctx context.Context, source Source, endpoint Endpoint) (Survey, error) {
	survey := Survey{Source: endpoint}
	paths, err := containerPaths(ctx, source.Catalog, endpoint)
	if err != nil {
		return Survey{}, err
	}
	for _, path := range paths {
		container, err := surveyContainer(ctx, source, path)
		if err != nil {
			return Survey{}, err
		}
		survey.Containers = append(survey.Containers, container)
	}
	survey.Throughput, survey.ThroughputKnown, err = readThroughput(ctx, source.Throughput, endpoint.Path)
	if err != nil {
		return Survey{}, err
	}
	return survey, nil
}

func containerPaths(ctx context.Context, catalog adapter.Catalog, endpoint Endpoint) ([][]string, error) {
	if endpoint.Container() {
		return [][]string{endpoint.Path}, nil
	}
	nodes, err := catalog.Children(ctx, databaseNode(endpoint.database()))
	if err != nil {
		return nil, fmt.Errorf("clone: list the containers of %s: %w", endpoint, err)
	}
	var paths [][]string
	for _, node := range nodes {
		if node.Kind == adapter.NodeContainer {
			paths = append(paths, node.Path)
		}
	}
	return paths, nil
}

func surveyContainer(ctx context.Context, source Source, path []string) (SourceContainer, error) {
	definition, err := source.Definitions.ContainerDefinition(ctx, path, adapter.DefinitionFull)
	if err != nil {
		return SourceContainer{}, fmt.Errorf("clone: read the definition of %s: %w", strings.Join(path, "."), err)
	}
	throughput, known, err := readThroughput(ctx, source.Throughput, path)
	if err != nil {
		return SourceContainer{}, err
	}
	return SourceContainer{Path: path, Definition: definition, Throughput: throughput, ThroughputKnown: known}, nil
}

func readThroughput(ctx context.Context, editor adapter.ThroughputEditor, path []string) (adapter.Throughput, bool, error) {
	if editor == nil {
		return adapter.Throughput{}, false, nil
	}
	throughput, err := editor.Throughput(ctx, path)
	if err != nil {
		return adapter.Throughput{}, false, fmt.Errorf("clone: read the throughput of %s: %w", strings.Join(path, "."), err)
	}
	return throughput, true, nil
}

func databaseNode(name string) adapter.Node {
	return adapter.Node{Kind: adapter.NodeDatabase, Name: name, Path: []string{name}, HasChildren: true}
}

// Plan is a job ready to run, as the review shows it: what it creates, and
// what it copies from where. It holds the connections the job borrowed.
type Plan struct {
	Job Job
	// CreatesDatabase is set when the target database does not exist yet;
	// Database is then what it is created as.
	CreatesDatabase bool
	Database        adapter.DatabaseSpec
	Containers      []ContainerPlan

	source Source
	target Target
}

// ContainerPlan is one container a job creates, and the source container
// it copies.
type ContainerPlan struct {
	Source []string
	Spec   adapter.ContainerSpec
	Size   adapter.SizeEstimate
}

func (c ContainerPlan) Target() []string { return []string{c.Spec.Database, c.Spec.Name} }

func (p Plan) Size() adapter.SizeEstimate {
	return totalSize(p.Containers, func(c ContainerPlan) adapter.SizeEstimate { return c.Size })
}

// Prepare turns a survey into a plan for job. It writes nothing anywhere.
// It fails with ErrTargetExists when the target is there already, checked
// through the target's catalog; the create is refused again by the service
// should one appear in between.
func Prepare(ctx context.Context, job Job, survey Survey, source Source, target Target) (Plan, error) {
	if job.Content == DefinitionAndItems && (source.Items == nil || target.Items == nil) {
		return Plan{}, fmt.Errorf("clone: %w", errNoItemAccess)
	}
	if job.Capacity == SameAsSource && !survey.ThroughputKnown {
		return Plan{}, fmt.Errorf("clone: %w", errNoThroughput)
	}
	databaseExists, err := checkTargetAbsent(ctx, target.Catalog, job.Target)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Job: job, CreatesDatabase: !databaseExists, source: source, target: target}
	plan.Database = adapter.DatabaseSpec{Name: job.Target.database()}
	if !job.Target.Container() {
		plan.Database.Throughput = databaseThroughput(job.Capacity, survey)
	}
	for _, container := range survey.Containers {
		planned, err := planContainer(ctx, job, container, source, databaseExists)
		if err != nil {
			return Plan{}, err
		}
		plan.Containers = append(plan.Containers, planned)
	}
	return plan, nil
}

// checkTargetAbsent reports whether the target's database exists, and fails
// when the target itself does.
func checkTargetAbsent(ctx context.Context, catalog adapter.Catalog, target Endpoint) (bool, error) {
	roots, err := catalog.Root(ctx)
	if err != nil {
		return false, fmt.Errorf("clone: list the databases of %s: %w", target.Account, err)
	}
	if !slices.ContainsFunc(roots, named(target.database())) {
		return false, nil
	}
	if !target.Container() {
		return true, fmt.Errorf("clone: %s: %w", target, ErrTargetExists)
	}
	containers, err := catalog.Children(ctx, databaseNode(target.database()))
	if err != nil {
		return true, fmt.Errorf("clone: list the containers of %s/%s: %w", target.Account, target.database(), err)
	}
	if slices.ContainsFunc(containers, named(target.Path[1])) {
		return true, fmt.Errorf("clone: %s: %w", target, ErrTargetExists)
	}
	return true, nil
}

func named(name string) func(adapter.Node) bool {
	return func(n adapter.Node) bool { return n.Name == name }
}

func planContainer(ctx context.Context, job Job, container SourceContainer, source Source, databaseExists bool) (ContainerPlan, error) {
	definition := container.Definition
	if job.Fidelity != adapter.DefinitionFull {
		var err error
		definition, err = source.Definitions.ContainerDefinition(ctx, container.Path, job.Fidelity)
		if err != nil {
			return ContainerPlan{}, fmt.Errorf("clone: read the definition of %s: %w", strings.Join(container.Path, "."), err)
		}
	}
	name := container.Path[1]
	if job.Target.Container() {
		name = job.Target.Path[1]
	}
	return ContainerPlan{
		Source: container.Path,
		Spec: adapter.ContainerSpec{
			Database:      job.Target.database(),
			Name:          name,
			PartitionKeys: definition.PartitionKeys,
			Throughput:    containerThroughput(job, container, databaseExists),
			Policies:      definition.Policies,
		},
		Size: definition.Size,
	}, nil
}

// databaseThroughput provisions the target database only where the source
// database had capacity of its own.
func databaseThroughput(capacity Capacity, survey Survey) adapter.Throughput {
	if !survey.Throughput.Provisioned() {
		return adapter.Throughput{}
	}
	return scaled(capacity, survey.Throughput)
}

// containerThroughput keeps a container drawing on its database doing so
// wherever the target database can have capacity to draw on: in a database
// clone, which copies that capacity, or in a database that exists. Anywhere
// else the capacity choice decides.
func containerThroughput(job Job, container SourceContainer, databaseExists bool) adapter.Throughput {
	shared := container.Throughput.Mode == adapter.ThroughputShared
	if shared && (!job.Target.Container() || databaseExists) {
		return adapter.Throughput{Mode: adapter.ThroughputShared}
	}
	if job.Capacity == SameAsSource && !container.Throughput.Provisioned() && !shared {
		return adapter.Throughput{}
	}
	return scaled(job.Capacity, container.Throughput)
}

func scaled(capacity Capacity, source adapter.Throughput) adapter.Throughput {
	switch capacity {
	case SameAsSource:
		if source.Provisioned() {
			return source
		}
	case None:
		return adapter.Throughput{}
	}
	return adapter.Throughput{Mode: adapter.ThroughputManual, RUs: MinimumRUs}
}

func (p Plan) CreateDatabase(ctx context.Context) error {
	if err := p.target.Admin.CreateDatabase(ctx, p.Database); err != nil {
		return createError(p.Job.Target.Account+"/"+p.Database.Name, err)
	}
	return nil
}

func (p Plan) CreateContainer(ctx context.Context, i int) error {
	container := p.Containers[i]
	if err := p.target.Admin.CreateContainer(ctx, container.Spec); err != nil {
		return createError(p.Job.Target.Account+"/"+strings.Join(container.Target(), "."), err)
	}
	return nil
}

func createError(target string, err error) error {
	if errors.Is(err, adapter.ErrAlreadyExists) {
		return fmt.Errorf("clone: create %s: %w: %w", target, ErrTargetExists, err)
	}
	return fmt.Errorf("clone: create %s: %w", target, err)
}
