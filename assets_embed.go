package vikramassets

import "embed"

// Workspace contains the built-in workspace templates shipped with vikram.
//
//go:embed workspace
var Workspace embed.FS
