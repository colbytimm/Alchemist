//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/tui"
)

// The database completion is tried against; its name shares no prefix with
// the other fixtures, so one typed prefix lists it alone.
const (
	completeDatabase  = "zcomplete_it"
	completeContainer = "orders"
	// tallHeight gives the editor room for the whole list.
	tallHeight = 40
)

// seedNested fills a container with items whose fields nest, so a completed
// path has somewhere to go.
func seedNested(t *testing.T) {
	t.Helper()
	client := seedClient(t)
	db := freshDatabase(t, client, completeDatabase)
	ctx := context.Background()
	_, err := db.CreateContainer(ctx, azcosmos.ContainerProperties{
		ID:                     completeContainer,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{Paths: []string{"/customerId"}},
	}, nil)
	require.NoError(t, err)
	container, err := client.NewContainer(completeDatabase, completeContainer)
	require.NoError(t, err)
	for i := range 5 {
		id := fmt.Sprintf("c%d", i)
		item := fmt.Sprintf(`{"id":%q,"customerId":%q,"customer":{"name":"customer %d","tier":%d}}`, id, id, i, i%2)
		_, err = container.CreateItem(ctx, azcosmos.NewPartitionKeyString(id), []byte(item), nil)
		require.NoError(t, err)
	}
}

// managed reports what a connection allows, the way cmd/ does.
func managed(conn adapter.Connection) tui.Management {
	admin, _ := conn.(adapter.CatalogAdmin)
	throughput, _ := conn.(adapter.ThroughputEditor)
	inspector, _ := conn.(adapter.Inspector)
	sampler, _ := conn.(adapter.FieldSampler)
	batcher, _ := conn.(adapter.Batcher)
	drafter, _ := conn.(adapter.ItemDrafter)
	definitions, _ := conn.(adapter.DefinitionReader)
	scanner, _ := conn.(adapter.ItemScanner)
	writer, _ := conn.(adapter.ItemWriter)
	return tui.Management{
		Admin:       admin,
		Throughput:  throughput,
		Inspector:   inspector,
		Sampler:     sampler,
		Batcher:     batcher,
		Drafter:     drafter,
		Definitions: definitions,
		Scanner:     scanner,
		Writer:      writer,
	}
}

// newCompletingSession is a session that samples fields, at a height that
// shows the whole list, logging to logged.
func newCompletingSession(t *testing.T, conn adapter.Connection, sampleFields bool, logged *bytes.Buffer) tea.Model {
	t.Helper()
	m := newSession(t, conn, tui.Options{
		Manage:       managed,
		SampleFields: sampleFields,
		Logger:       log.New(logged),
	})
	m, _ = m.Update(tea.WindowSizeMsg{Width: testWidth, Height: tallHeight})
	return m
}

// listed reports whether the editor pane shows text on a suggestion row.
func listed(m tea.Model, text string) bool {
	for _, line := range strings.Split(view(m), "\n") {
		if strings.Contains(line, "▸ "+text+" ") || strings.Contains(line, "  "+text+" ") {
			return true
		}
	}
	return false
}

// accept narrows the list with prefix, walks down to want, and takes it.
func accept(t *testing.T, m tea.Model, prefix, want string) tea.Model {
	t.Helper()
	m = press(t, m, keyText(prefix))
	require.True(t, listed(m, want), "typing %q should list %q:\n%s", prefix, want, view(m))
	for tries := 0; !strings.Contains(view(m), "▸ "+want+" "); tries++ {
		require.Less(t, tries, 20, "%q is listed but cannot be chosen:\n%s", want, view(m))
		m = press(t, m, keyMsg(tea.KeyDown))
	}
	return press(t, m, keyMsg(tea.KeyTab))
}

func TestIntegrationCompletion(t *testing.T) {
	conn := connectWithRetry(t)
	seedNested(t)
	seedSales(t)

	t.Run("a database, a container, and a nested field in one query", func(t *testing.T) {
		var logged bytes.Buffer
		m := press(t, newCompletingSession(t, conn, true, &logged), keyRune('e'), keyText("SELECT * FROM "))

		m = accept(t, m, "zcomp", completeDatabase)
		m = accept(t, m, ".", completeContainer)
		m = press(t, m, keyText(" c WHERE c."))
		m = accept(t, m, "cust", "customer")
		m = accept(t, m, ".", "name")
		m = press(t, m, keyText(" = 'customer 1'"), keyMsg(tea.KeyCtrlR))

		rendered := view(m)
		assert.Contains(t, rendered, "SELECT * FROM zcomplete_it.orders c WHERE", "the editor wraps the rest")
		assert.Contains(t, rendered, "c.customer.name = 'customer 1'")
		assert.Equal(t, 1.0, number(t, rowsPattern, rendered), "the completed query ran and found its row")
		assert.Contains(t, logged.String(), "sampled fields", "the sample and its charge are in the log")
	})

	t.Run("the join example through completion", func(t *testing.T) {
		var logged bytes.Buffer
		m := press(t, newCompletingSession(t, conn, true, &logged), keyRune('e'), keyText("SELECT * FROM "))

		m = accept(t, m, "alchemist_j", joinDatabase)
		m = accept(t, m, ".", "orders")
		m = press(t, m, keyText(" o JOIN "))
		m = accept(t, m, "alchemist_j", joinDatabase)
		m = accept(t, m, ".", "customers")
		m = press(t, m, keyText(" cu ON o."))
		m = accept(t, m, "cust", "customerId")
		m = press(t, m, keyText(" = cu."))
		m = accept(t, m, "i", "id")
		m = press(t, m, keyText(" WHERE cu."))
		m = accept(t, m, "na", "name")
		m = press(t, m, keyText(" = 'customer 1'"), keyMsg(tea.KeyCtrlR))

		rendered := view(m)
		assert.Contains(t, rendered, "cu ON o.customerId", "the editor wraps the rest")
		assert.Contains(t, rendered, "= cu.id WHERE cu.name = 'customer 1'")
		assert.Contains(t, rendered, "simulated (client-side)")
		assert.Greater(t, number(t, rowsPattern, rendered), 0.0)
		assert.Equal(t, 2, strings.Count(logged.String(), "sampled fields"), "one sample per container")
	})

	t.Run("sample_fields = false issues no sampling query", func(t *testing.T) {
		var logged bytes.Buffer
		m := press(t, newCompletingSession(t, conn, false, &logged), keyRune('e'))

		m = press(t, m, keyText("SELECT * FROM "+completeDatabase+"."+completeContainer+" c WHERE c."))

		assert.True(t, listed(m, "customerId"), "the partition key is still known:\n%s", view(m))
		assert.False(t, listed(m, "customer"))
		assert.NotContains(t, logged.String(), "sample")
	})
}
