//go:build race

package gml

// raceBuild: the race detector instruments allocations, so allocation
// budgets do not hold under it.
const raceBuild = true
