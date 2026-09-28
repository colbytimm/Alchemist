package sample

import "fmt"

const (
	customerCount = 12
	// Orders name a few customers that do not exist, so an inner join has
	// rows to drop.
	customersNamed = customerCount + 3
	orderCount     = 60
	archiveCount   = 20
	productCount   = 15
	deviceCount    = 10
	// eventCount spans several pages, and passes a max_join_rows of 100.
	eventCount      = 300
	alertCount      = 25
	departmentCount = 4
	employeeCount   = 18
	teamSize        = 6
)

var (
	regions    = []string{"east", "north", "south", "west"}
	statuses   = []string{"open", "shipped", "cancelled"}
	cities     = []string{"Calgary", "Lisbon", "Osaka", "Nairobi", "Lima"}
	categories = []string{"glassware", "reagents", "instruments"}
	severities = []string{"info", "warning", "critical"}
	eventKinds = []string{"temperature", "pressure", "heartbeat"}
)

func databases() []database {
	return []database{
		{name: "sales", containers: []container{
			{name: "customers", partitionKey: "region", items: customers()},
			{name: "orders", partitionKey: "customerId", items: orders("o", orderCount, 0)},
			{name: "archive", partitionKey: "customerId", items: archivedOrders()},
			{name: "products", partitionKey: "category", items: products()},
		}},
		{name: "telemetry", containers: []container{
			{name: "devices", partitionKey: "site", items: devices()},
			{name: "events", partitionKey: "deviceId", items: events()},
			{name: "alerts", partitionKey: "severity", items: alerts()},
		}},
		{name: "hr", containers: []container{
			{name: "departments", partitionKey: "site", items: departments()},
			{name: "employees", partitionKey: "site", items: employees()},
		}},
	}
}

func pick(values []string, i int) string {
	return values[i%len(values)]
}

// customers include one no order names, past the ones orders do, so a full
// join has unmatched rows on both sides.
func customers() []item {
	var items []item
	for i := range customerCount {
		items = append(items, customer(i))
	}
	return append(items, customer(customersNamed))
}

func customer(i int) item {
	return item{
		"id":     fmt.Sprintf("c%02d", i),
		"name":   fmt.Sprintf("Customer %02d", i),
		"region": pick(regions, i),
		"tier":   pick([]string{"bronze", "silver", "gold"}, i),
		"vip":    i%5 == 0,
	}
}

func orders(prefix string, count, offset int) []item {
	var items []item
	for i := range count {
		n := i + offset
		items = append(items, item{
			"id":         fmt.Sprintf("%s%03d", prefix, n),
			"customerId": fmt.Sprintf("c%02d", n%customersNamed),
			// sku repeats the first line's, so a cross-container join, which
			// cannot reach into lines, has a key for products.
			"sku":    fmt.Sprintf("p%02d", n%productCount),
			"status": pick(statuses, n),
			"total":  float64(n%17)*12.5 + 20,
			"shipTo": item{"city": pick(cities, n), "region": pick(regions, n)},
			"tags":   []string{pick(categories, n), pick(statuses, n+1)},
			"lines": []item{
				{"sku": fmt.Sprintf("p%02d", n%productCount), "quantity": n%4 + 1},
			},
		})
	}
	return items
}

// archivedOrders have a field live orders lack, so a union of the two grows a
// column when it crosses into the archive.
func archivedOrders() []item {
	items := orders("a", archiveCount, orderCount)
	for i, it := range items {
		it["archivedAt"] = fmt.Sprintf("2025-%02d-01T00:00:00Z", i%12+1)
	}
	return items
}

func products() []item {
	var items []item
	for i := range productCount {
		items = append(items, item{
			"id":       fmt.Sprintf("p%02d", i),
			"name":     fmt.Sprintf("Product %02d", i),
			"category": pick(categories, i),
			"price":    float64(i)*3.25 + 5,
		})
	}
	return items
}

func devices() []item {
	var items []item
	for i := range deviceCount {
		items = append(items, item{
			"id":       fmt.Sprintf("d%02d", i),
			"site":     pick(cities, i),
			"model":    pick([]string{"TX-1", "TX-2"}, i),
			"firmware": fmt.Sprintf("1.%d.0", i%3),
		})
	}
	return items
}

func events() []item {
	var items []item
	for i := range eventCount {
		items = append(items, item{
			"id":       fmt.Sprintf("e%04d", i),
			"deviceId": fmt.Sprintf("d%02d", i%deviceCount),
			"kind":     pick(eventKinds, i),
			"value":    float64(i%40) + 0.5,
			"at":       fmt.Sprintf("2026-01-%02dT%02d:00:00Z", i%28+1, i%24),
		})
	}
	return items
}

func alerts() []item {
	var items []item
	for i := range alertCount {
		items = append(items, item{
			"id":       fmt.Sprintf("al%02d", i),
			"deviceId": fmt.Sprintf("d%02d", (i*3)%(deviceCount+2)),
			"severity": pick(severities, i),
			"message":  fmt.Sprintf("%s out of range", pick(eventKinds, i)),
			"open":     i%2 == 0,
		})
	}
	return items
}

// departments and employees join on a number rather than a string.
func departments() []item {
	var items []item
	for i := range departmentCount {
		items = append(items, item{
			"id":     fmt.Sprintf("dept-%d", i+1),
			"number": i + 1,
			"name":   pick([]string{"Research", "Operations", "Finance", "Field"}, i),
			"site":   pick(cities, i),
		})
	}
	return items
}

// employees report in teams of teamSize to the first of each team, who
// reports to no one, so a self-join on managerId pads the managers.
func employees() []item {
	var items []item
	for i := range employeeCount {
		employee := item{
			"id":               fmt.Sprintf("emp-%02d", i),
			"name":             fmt.Sprintf("Employee %02d", i),
			"departmentNumber": i%(departmentCount+1) + 1,
			"site":             pick(cities, i),
			"manager":          i%teamSize == 0,
		}
		if i%teamSize != 0 {
			employee["managerId"] = fmt.Sprintf("emp-%02d", i/teamSize*teamSize)
		}
		items = append(items, employee)
	}
	return items
}
