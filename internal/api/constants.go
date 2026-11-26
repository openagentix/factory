// Package api holds the shared types of the factory: task and factory files,
// the worker result, the evidence bundle, the run record and run events.
//
// The names below are placeholders and may change. They live here and
// nowhere else. Other code must use these constants.
package api

const (
	// Group is the API group of every factory resource.
	Group = "factory.openagentix.org"
	// Version is the schema version of the API group.
	Version = "v1alpha1"
	// APIVersion is the value of the apiVersion field in resource files.
	APIVersion = Group + "/" + Version
	// ProductName is the product name shown to people.
	ProductName = "OpenAgentix Software Factory"
)

// Resource kinds.
const (
	KindTask    = "Task"
	KindFactory = "Factory"
)
