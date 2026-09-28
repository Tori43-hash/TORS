package tors

import "context"

// The lifecycle interfaces are optional; a module implements what it needs.
// Order: New → unmarshal config → Provision → Validate → (apps) Start … Stop → Cleanup.

// Provisioner sets a module up: resolves dependencies, loads guest modules,
// opens resources. After Provision the module must be ready to serve calls.
type Provisioner interface {
	Provision(Context) error
}

// Validator checks the provisioned module. It must not have side effects.
type Validator interface {
	Validate() error
}

// CleanerUpper releases what Provision acquired.
type CleanerUpper interface {
	Cleanup() error
}

// App is a top-level module with background activity. Start must not block;
// Stop must be idempotent. Top-level modules without background work need
// not implement it.
type App interface {
	Start() error
	Stop() error
}

// HealthChecker reports whether a module can do its job right now.
type HealthChecker interface {
	Health(context.Context) error
}
