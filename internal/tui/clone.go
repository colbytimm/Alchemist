package tui

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/clone"
	"github.com/colbytimm/alchemist/internal/tui/panes"
	"github.com/colbytimm/alchemist/internal/writers"
)

// clonePrompt is a clone between y and the review's confirmation: where it
// reads from, what can read there, the target the form is connecting, and
// the plan the review restates.
type clonePrompt struct {
	source     clone.Endpoint
	reader     clone.Source
	connecting string
	plan       clone.Plan
}

// openClone opens the clone prompt for the node under the cursor, and reads
// its source. Nothing is written until the review is confirmed.
func (m Model) openClone() (Model, tea.Cmd) {
	entry, connected := m.activeConnection()
	node, selected := m.catalogPane().SelectedNode()
	management := entry.management
	if !connected || !selected || node.Kind == adapter.NodeField || management.Definitions == nil {
		return m, nil
	}
	source := clone.Endpoint{Account: entry.account.Name, Path: node.Path}
	reader := clone.Source{
		Catalog:     entry.catalog,
		Definitions: management.Definitions,
		Throughput:  management.Throughput,
		Items:       management.Scanner,
	}
	targets, readOnly := m.cloneTargets()
	m.dialog++
	m.clonePrompt = clonePrompt{source: source, reader: reader}
	m.cloneForm = panes.NewCloneForm(m.icons, source, targets, readOnly, reader.Items != nil).SetSize(m.width, m.height)
	m = m.applyCloneDefaults()
	m.overlay = overlayCloneForm
	return m, surveyClone(m.dialog, reader, source)
}

// cloneTargets are the accounts a clone may write to, in the switcher's
// order, and the names of the read-only ones left out.
func (m Model) cloneTargets() ([]panes.CloneTarget, []string) {
	var targets []panes.CloneTarget
	var readOnly []string
	for _, name := range m.accounts.names() {
		entry, _ := m.accounts.get(name)
		if entry.account.ReadOnly {
			readOnly = append(readOnly, name)
			continue
		}
		targets = append(targets, panes.CloneTarget{Name: name, State: entry.state})
	}
	return targets, readOnly
}

// applyCloneDefaults sets what the form's untouched fields default to for
// the target account it shows now.
func (m Model) applyCloneDefaults() Model {
	target := m.cloneForm.Target()
	if target == "" {
		return m
	}
	source := m.clonePrompt.source
	defaults := panes.CloneDefaults{
		Name:     panes.CopyName(source, target),
		Fidelity: adapter.DefinitionFull,
		Capacity: m.defaultCapacity(),
	}
	if target != source.Account {
		defaults.Fidelity = adapter.DefinitionPortable
	}
	m.cloneForm = m.cloneForm.ApplyDefaults(defaults)
	return m
}

// defaultCapacity is Minimum, so a source at 40,000 RU/s is never copied at
// that price by pressing enter twice, unless the source provisions nothing
// or draws on a database the copy can draw on too.
func (m Model) defaultCapacity() clone.Capacity {
	survey := m.cloneForm.Survey()
	if !m.cloneForm.Surveyed() || !survey.Source.Container() || len(survey.Containers) != 1 {
		return clone.Minimum
	}
	source := survey.Containers[0]
	switch {
	case !source.ThroughputKnown:
		return clone.Minimum
	case source.Throughput.Mode == adapter.ThroughputNone:
		return clone.None
	case source.Throughput.Mode == adapter.ThroughputShared && m.targetDatabaseListed():
		return clone.None
	}
	return clone.Minimum
}

// targetDatabaseListed reports whether the target's tree has listed the
// database the form names, which is as much as the session knows without
// asking.
func (m Model) targetDatabaseListed() bool {
	choice := m.cloneForm.Choice()
	entry, ok := m.accounts.get(choice.Target.Account)
	if !ok || !entry.connected() {
		return false
	}
	_, listed := entry.pane.Node(choice.Target.Path[:1])
	return listed
}

func surveyClone(dialog dialogID, reader clone.Source, source clone.Endpoint) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), manageTimeout)
		defer cancel()
		survey, err := clone.SurveySource(ctx, reader, source)
		return ClonePreparedMsg{Survey: survey, Err: err, dialog: dialog}
	}
}

func (m Model) fileSurvey(msg ClonePreparedMsg) Model {
	if msg.dialog != m.dialog || m.overlay != overlayCloneForm {
		return m
	}
	if msg.Err != nil {
		m.logger.Error("clone source not read", "source", m.clonePrompt.source, "error", msg.Err)
		m.cloneForm = m.cloneForm.Fail(msg.Err)
		return m
	}
	m.cloneForm = m.cloneForm.SetSurvey(msg.Survey)
	return m.applyCloneDefaults()
}

// handleCloneFormKey drives the prompt. Typed characters belong to its
// fields, which is why q cannot quit here.
func (m Model) handleCloneFormKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if typesIntoBuffer(msg) {
		return m.cloneFormUpdate(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Close):
		return m.closeClonePrompt(), nil
	case key.Matches(msg, m.keys.Save):
		return m.submitCloneForm()
	case key.Matches(msg, m.keys.NextPane), key.Matches(msg, m.keys.Down):
		m.cloneForm = m.cloneForm.NextField()
		return m, nil
	case key.Matches(msg, m.keys.PrevPane), key.Matches(msg, m.keys.Up):
		m.cloneForm = m.cloneForm.PrevField()
		return m, nil
	}
	return m.cloneFormUpdate(msg)
}

func (m Model) cloneFormUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.cloneForm, cmd = m.cloneForm.Update(msg)
	return m.applyCloneDefaults(), cmd
}

// closeClonePrompt drops the prompt. A target it was connecting carries on,
// and stays connected in the background, like any account visited and left.
func (m Model) closeClonePrompt() Model {
	m.clonePrompt = clonePrompt{}
	m.overlay = overlayNone
	return m
}

// submitCloneForm moves to the review, connecting the target first when it
// is not connected: the session stays where it is meanwhile.
func (m Model) submitCloneForm() (Model, tea.Cmd) {
	if m.cloneForm.Waiting() {
		return m, nil
	}
	if err := m.cloneForm.Validate(); err != nil {
		m.cloneForm = m.cloneForm.Fail(err)
		return m, nil
	}
	target, _ := m.accounts.get(m.cloneForm.Target())
	if !target.connected() {
		return m.connectCloneTarget(target)
	}
	return m.planCloneOn(target)
}

func (m Model) connectCloneTarget(target accountEntry) (Model, tea.Cmd) {
	name := target.account.Name
	m.clonePrompt.connecting = name
	m.cloneForm = m.cloneForm.SetStatus(fmt.Sprintf("connecting %s…", name))
	if target.state == panes.AccountConnecting {
		return m, nil
	}
	m = m.startConnecting(name)
	m, tick := m.syncAccountRows()
	target, _ = m.accounts.get(name)
	return m, tea.Batch(tick, m.connectAccount(target))
}

// continueClonePrompt plans the clone once the target the form was
// connecting has connected.
func (m Model) continueClonePrompt(account string) (Model, tea.Cmd) {
	entry, _ := m.accounts.get(account)
	if m.clonePrompt.connecting != account || m.overlay != overlayCloneForm || !entry.connected() {
		return m, nil
	}
	m.clonePrompt.connecting = ""
	return m.planCloneOn(entry)
}

// failClonePrompt shows why the target the form was connecting did not
// connect. A missing key is not asked for here: an overlay does not open an
// overlay.
func (m Model) failClonePrompt(msg ErrMsg) Model {
	if m.clonePrompt.connecting != msg.Account || m.overlay != overlayCloneForm {
		return m
	}
	m.clonePrompt.connecting = ""
	if errors.Is(msg.Err, ErrCredentialsNeeded) {
		m.cloneForm = m.cloneForm.Fail(fmt.Errorf("%s has no key: add it from the account switcher (ctrl+g), then clone again", msg.Account))
		return m
	}
	m.cloneForm = m.cloneForm.Fail(fmt.Errorf("%s did not connect: %w", msg.Account, msg.Err))
	return m
}

// planCloneOn checks that target can take the clone the form describes, and
// asks it whether the name is free.
func (m Model) planCloneOn(target accountEntry) (Model, tea.Cmd) {
	choice := m.cloneForm.Choice()
	permitted := target.permitted()
	name := target.account.Name
	switch {
	case permitted.Admin == nil:
		m.cloneForm = m.cloneForm.Fail(fmt.Errorf("%s cannot create containers", name))
		return m, nil
	case choice.Content == clone.DefinitionAndItems && permitted.Writer == nil:
		m.cloneForm = m.cloneForm.Fail(fmt.Errorf("%s cannot write items: copy the definition only", name))
		return m, nil
	}
	cloneJob := clone.Job{
		Source:   m.clonePrompt.source,
		Target:   choice.Target,
		Content:  choice.Content,
		Fidelity: choice.Fidelity,
		Capacity: choice.Capacity,
		Writers:  writersFor(target.account),
	}
	writer := clone.Target{Catalog: target.catalog, Admin: permitted.Admin, Items: permitted.Writer}
	m.cloneForm = m.cloneForm.SetStatus(fmt.Sprintf("checking %s…", choice.Target))
	return m, planClone(m.dialog, cloneJob, m.cloneForm.Survey(), m.clonePrompt.reader, writer)
}

// writersFor is how many writes a clone into account keeps in flight.
func writersFor(account Account) int {
	if account.Writers <= 0 {
		return writers.DefaultSize
	}
	return account.Writers
}

func planClone(dialog dialogID, job clone.Job, survey clone.Survey, source clone.Source, target clone.Target) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), manageTimeout)
		defer cancel()
		plan, err := clone.Prepare(ctx, job, survey, source, target)
		return ClonePlannedMsg{Plan: plan, Err: err, dialog: dialog}
	}
}

// openCloneReview restates the plan and asks for the target account's
// name, or shows why there is no plan under the fields.
func (m Model) openCloneReview(msg ClonePlannedMsg) Model {
	if msg.dialog != m.dialog || m.overlay != overlayCloneForm {
		return m
	}
	if msg.Err != nil {
		m.cloneForm = m.cloneForm.Fail(msg.Err)
		return m
	}
	m.cloneForm = m.cloneForm.SetStatus("")
	m.clonePrompt.plan = msg.Plan
	m.cloneReview = panes.NewCloneReview(m.icons, msg.Plan).SetSize(m.width, m.height)
	m.overlay = overlayCloneReview
	return m
}

// handleCloneReviewKey drives the review: esc goes back to the form as it
// was left, and enter starts nothing until the account's name is typed.
func (m Model) handleCloneReviewKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if typesIntoBuffer(msg) {
		return m.cloneReviewUpdate(msg)
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.quit()
	case tea.KeyEsc:
		m.overlay = overlayCloneForm
		return m, nil
	case tea.KeyEnter:
		if !m.cloneReview.Confirmed() || m.job.active() {
			return m, nil
		}
		return m.startClone(m.clonePrompt.plan)
	}
	return m.cloneReviewUpdate(msg)
}

func (m Model) cloneReviewUpdate(msg tea.KeyMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.cloneReview, cmd = m.cloneReview.Update(msg)
	return m, cmd
}
