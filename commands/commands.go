package commands

import _ "embed"

// HydrogenToml contains the Antigravity slash command template for Hydrogen 4.
//
//go:embed hydrogen.toml
var HydrogenToml string

// ArgonToml contains the Antigravity slash command template for Argon 4.
//
//go:embed argon.toml
var ArgonToml string
