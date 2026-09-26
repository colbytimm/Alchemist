//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/query"
)

// The fixture of this file. Orders name customers c0 to c4, of which c4 does
// not exist, and customer c9 is named by no order. Every fourth order has
// no lines, and lines name products s0 to s2, of which s2 does not exist.
// Only device d0 has alerts. Employees report in teams of three.
const (
	outerDatabase   = "e20_joins"
	outerOrders     = 12
	outerCustomers  = 4
	namedCustomers  = outerCustomers + 1
	lapsedCustomer  = "c9"
	outerProducts   = 2
	namedProducts   = outerProducts + 1
	outerDevices    = 4
	outerAlerts     = 3
	outerEmployees  = 9
	outerTeamSize   = 3
	outerDepartment = 2
)

func seedOuter(t *testing.T) adapter.Connection {
	t.Helper()
	conn := connectWithRetry(t)
	client := seedClient(t)
	freshDatabase(t, client, outerDatabase)
	var orders, customers, products, devices, alerts, departments, employees []map[string]any
	for i := range outerOrders {
		order := map[string]any{
			"id": fmt.Sprintf("o%02d", i), "region": regions[i%len(regions)],
			"customerId": fmt.Sprintf("c%d", i%namedCustomers), "total": i * 10,
			"tags": []string{"t" + fmt.Sprint(i%2)},
		}
		if i%4 != 0 {
			order["lines"] = []map[string]any{{"sku": fmt.Sprintf("s%d", i%namedProducts), "quantity": i%3 + 1}}
		}
		orders = append(orders, order)
	}
	for i := range outerCustomers {
		customers = append(customers, map[string]any{"id": fmt.Sprintf("c%d", i), "region": regions[i%len(regions)], "name": fmt.Sprintf("customer %d", i)})
	}
	customers = append(customers, map[string]any{"id": lapsedCustomer, "region": "east", "name": "lapsed"})
	for i := range outerProducts {
		products = append(products, map[string]any{"id": fmt.Sprintf("s%d", i), "region": "east", "name": fmt.Sprintf("product %d", i)})
	}
	for i := range outerDevices {
		devices = append(devices, map[string]any{"id": fmt.Sprintf("d%d", i), "region": "east", "site": fmt.Sprintf("site %d", i)})
	}
	for i := range outerAlerts {
		alerts = append(alerts, map[string]any{"id": fmt.Sprintf("al%d", i), "region": "east", "deviceId": "d0", "message": fmt.Sprintf("alert %d", i)})
	}
	for i := range outerDepartment {
		departments = append(departments, map[string]any{"id": fmt.Sprintf("dept%d", i), "region": "east", "name": fmt.Sprintf("department %d", i)})
	}
	for i := range outerEmployees {
		employee := map[string]any{"id": fmt.Sprintf("e%d", i), "region": "east", "name": fmt.Sprintf("employee %d", i), "manager": i%outerTeamSize == 0}
		if i%outerTeamSize != 0 {
			employee["managerId"] = fmt.Sprintf("e%d", i/outerTeamSize*outerTeamSize)
		}
		employees = append(employees, employee)
	}
	for name, items := range map[string][]map[string]any{
		"orders": orders, "customers": customers, "products": products, "devices": devices,
		"alerts": alerts, "departments": departments, "employees": employees,
	} {
		seedOuterContainer(t, client, name, items)
	}
	return conn
}

func seedOuterContainer(t *testing.T, client *azcosmos.Client, name string, items []map[string]any) {
	t.Helper()
	db, err := client.NewDatabase(outerDatabase)
	require.NoError(t, err)
	_, err = db.CreateContainer(context.Background(), azcosmos.ContainerProperties{
		ID:                     name,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{Paths: []string{"/region"}},
	}, nil)
	require.NoError(t, err)
	container, err := client.NewContainer(outerDatabase, name)
	require.NoError(t, err)
	for _, item := range items {
		body, err := json.Marshal(item)
		require.NoError(t, err)
		region, ok := item["region"].(string)
		require.True(t, ok)
		_, err = container.CreateItem(context.Background(), azcosmos.NewPartitionKeyString(region), body, nil)
		require.NoError(t, err)
	}
}

// outerRun is one query's result: its rows, sorted, with the charge of its
// pages and of each leaf.
type outerRun struct {
	rows   []string
	charge float64
	leaves map[string]float64
}

func runOuter(t *testing.T, conn adapter.Connection, text string) outerRun {
	t.Helper()
	plan, err := query.BuildPlan(strings.ReplaceAll(text, "db.", outerDatabase+"."))
	require.NoError(t, err)
	cursor, err := query.Engine{Connection: conn}.Execute(context.Background(), plan)
	require.NoError(t, err)
	run := outerRun{leaves: map[string]float64{}}
	var leafTotal float64
	for _, page := range readAll(t, cursor) {
		for _, row := range page.Rows {
			run.rows = append(run.rows, strings.Join(row, " "))
		}
		run.charge += page.Stats.RequestCharge
		for leaf, charge := range page.Stats.LeafCharges {
			run.leaves[strings.ReplaceAll(leaf, outerDatabase+".", "db.")] += charge
			leafTotal += charge
		}
	}
	slices.Sort(run.rows)
	if plan.Simulated() {
		assert.InDelta(t, run.charge, leafTotal, 0.01, "the charge is the sum of its leaves")
	}
	return run
}

func sorted(rows []string) []string {
	slices.Sort(rows)
	return rows
}

func TestIntegrationOuterJoins(t *testing.T) {
	conn := seedOuter(t)
	var leftRows, missing, fullRows []string
	for i := range outerOrders {
		customer := i % namedCustomers
		if customer == outerCustomers {
			missing = append(missing, fmt.Sprintf("o%02d ", i))
			leftRows = append(leftRows, fmt.Sprintf("o%02d ", i))
			continue
		}
		leftRows = append(leftRows, fmt.Sprintf("o%02d customer %d", i, customer))
	}
	fullRows = append(slices.Clone(leftRows), " lapsed")
	var devices []string
	for i := range outerAlerts {
		devices = append(devices, fmt.Sprintf("alert %d site 0", i))
	}
	for i := 1; i < outerDevices; i++ {
		devices = append(devices, fmt.Sprintf(" site %d", i))
	}
	const orderCustomer = "SELECT o.id, cu.name FROM db.orders o %s db.customers cu ON o.customerId = cu.id"
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "left", text: fmt.Sprintf(orderCustomer, "LEFT JOIN"), want: leftRows},
		{name: "left, absent side only", text: fmt.Sprintf(orderCustomer, "LEFT JOIN") + " WHERE NOT IS_DEFINED(cu)", want: missing},
		{name: "full", text: fmt.Sprintf(orderCustomer, "FULL JOIN"), want: fullRows},
		{name: "right", text: "SELECT a.message, d.site FROM db.alerts a RIGHT JOIN db.devices d ON a.deviceId = d.id", want: devices},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, sorted(tt.want), runOuter(t, conn, tt.text).rows)
		})
	}
}

func TestIntegrationApplyAndCrossJoin(t *testing.T) {
	conn := seedOuter(t)
	var lines, joined []string
	for i := range outerOrders {
		if i%4 == 0 {
			continue
		}
		sku := i % namedProducts
		lines = append(lines, fmt.Sprintf("o%02d", i))
		if sku < outerProducts {
			joined = append(joined, fmt.Sprintf("o%02d %d product %d", i, i%3+1, sku))
		}
	}

	applied := runOuter(t, conn, "SELECT o.id, l.quantity, p.name FROM db.orders o OUTER APPLY l IN o.lines JOIN db.products p ON l.sku = p.id")
	lone := runOuter(t, conn, "SELECT o.id, l.sku FROM db.orders o CROSS APPLY l IN o.lines")
	written := runOuter(t, conn, "SELECT o.id, l.sku FROM db.orders o JOIN l IN o.lines")
	cross := runOuter(t, conn, `SELECT d.name, e.name AS employee FROM db.departments d CROSS JOIN db.employees e WHERE e.manager = true`)

	assert.Equal(t, sorted(joined), applied.rows)
	assert.Len(t, lone.rows, len(lines))
	assert.Equal(t, written.rows, lone.rows, "a lone CROSS APPLY is the service's JOIN ... IN")
	assert.Len(t, cross.rows, outerDepartment*outerEmployees/outerTeamSize)
}

func TestIntegrationCTEs(t *testing.T) {
	conn := seedOuter(t)
	projected := runOuter(t, conn, `WITH named AS (SELECT cu.id, cu.name FROM db.customers cu),
     big AS (SELECT o.id, o.customerId FROM db.orders o WHERE o.total > 30)
SELECT big.id, named.name FROM big JOIN named ON big.customerId = named.id`)
	whole := runOuter(t, conn, "SELECT o.id, cu.name FROM db.orders o JOIN db.customers cu ON o.customerId = cu.id WHERE o.total > 30")
	staffed := runOuter(t, conn, `WITH staff AS (SELECT e.id, e.name, e.managerId FROM db.employees e)
SELECT w.name, m.name AS manager FROM staff w LEFT JOIN staff m ON w.managerId = m.id`)
	scan := runOuter(t, conn, "SELECT e.id, e.name, e.managerId FROM db.employees e")

	assert.Equal(t, whole.rows, projected.rows)
	// The vnext-preview emulator charges a page the same whatever it holds,
	// so a projection cannot show as cheaper here; it must not cost more.
	assert.LessOrEqual(t, projected.leaves["named (db.customers)"], whole.leaves["db.customers"])
	assert.LessOrEqual(t, projected.leaves["big (db.orders)"], whole.leaves["db.orders"])
	assert.Len(t, staffed.rows, outerEmployees)
	assert.InDelta(t, scan.charge, staffed.leaves["staff (db.employees)"], 0.01, "the CTE read twice is scanned once")
}
