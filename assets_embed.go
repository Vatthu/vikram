package levikassets

import "embed"

// Workspace contains the built-in workspace templates shipped with levik.
//
//go:embed workspace
var Workspace embed.FS
