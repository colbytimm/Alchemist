package panes

import (
	"fmt"
	"strings"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/clone"
	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	cloneReviewHint   = "enter clone · esc back"
	cloneReviewPrompt = "Type the target account name to confirm:"
	notCopied         = "Not copied: stored procedures, triggers, functions, _ts and _etag values, change feed history."
	billed            = "On a billed account that costs money until it is deleted."
)

// NewCloneReview restates plan and asks for the target account's name back:
// the one field of the form that was cycled rather than typed, and the one
// most expensive to get wrong.
func NewCloneReview(icons theme.IconSet, plan clone.Plan) Confirm {
	title := cloneContainerTitle
	if !plan.Job.Source.Container() {
		title = cloneDatabaseTitle
	}
	return Confirm{
		frame:       frame{title: title, focused: true},
		icons:       icons,
		consequence: strings.Join(reviewParagraphs(plan), "\n\n"),
		prompt:      cloneReviewPrompt,
		hint:        cloneReviewHint,
		name:        newNameField(plan.Job.Target.Account),
	}
}

func reviewParagraphs(plan clone.Plan) []string {
	job := plan.Job
	source, target := job.Source, job.Target
	paragraphs := []string{endpointText(source) + "  →  " + endpointText(target)}
	paragraphs = append(paragraphs, strings.Join(creations(plan), " "))
	paragraphs = append(paragraphs, copies(plan))
	if job.Content == clone.DefinitionAndItems && source.Account != target.Account {
		paragraphs = append(paragraphs, fmt.Sprintf("Items leave %s.", source.Account))
	}
	return append(paragraphs, notCopied)
}

func endpointText(e clone.Endpoint) string {
	return e.Account + " / " + strings.Join(e.Path, ".")
}

func creations(plan clone.Plan) []string {
	target := plan.Job.Target
	var lines []string
	if plan.CreatesDatabase {
		lines = append(lines, fmt.Sprintf("Creates database %s on %s (it does not exist)%s.",
			plan.Database.Name, target.Account, databaseCapacity(plan.Database.Throughput)))
	}
	provisioned := plan.Database.Throughput.Provisioned()
	for _, container := range plan.Containers {
		lines = append(lines, fmt.Sprintf("Creates container %s%s.", container.Spec.Name, containerCapacity(container.Spec.Throughput)))
		provisioned = provisioned || container.Spec.Throughput.Provisioned()
	}
	if provisioned {
		lines = append(lines, billed)
	}
	return lines
}

func databaseCapacity(t adapter.Throughput) string {
	if !t.Provisioned() {
		return ", with no throughput of its own"
	}
	return capacityText(t)
}

func containerCapacity(t adapter.Throughput) string {
	switch t.Mode {
	case adapter.ThroughputShared:
		return " drawing on its database's throughput"
	case adapter.ThroughputNone:
		return " with no throughput of its own"
	}
	return capacityText(t)
}

func capacityText(t adapter.Throughput) string {
	if t.Mode == adapter.ThroughputAutoscale {
		return fmt.Sprintf(" at up to %d RU/s autoscale", t.RUs)
	}
	return fmt.Sprintf(" at %d RU/s manual", t.RUs)
}

func copies(plan clone.Plan) string {
	if plan.Job.Content == clone.DefinitionOnly {
		return "Copies the definition only: no item is read or written."
	}
	size := plan.Size()
	count := "about " + FormatCount(size.Items) + " items"
	switch {
	case !size.Known:
		count = "an unknown number of items: the account did not say how many"
	case size.Items == 0:
		count = "whatever items it finds, though the account reports none"
	}
	return fmt.Sprintf("Copies %s. Items changed on %s while the copy runs may or may not be included.",
		count, plan.Job.Source.Account)
}
