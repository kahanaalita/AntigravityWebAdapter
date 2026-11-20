package commands

import _ "embed"

// HydrogenToml contains the Antigravity slash command template for Hydrogen 4.
//
//go:embed hydrogen.toml
var HydrogenToml string

// NeonToml contains the Antigravity slash command template for Neon 4.
//
//go:embed neon.toml
var NeonToml string
