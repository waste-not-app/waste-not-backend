package contracts

import "embed"

//go:embed ai/*.json android/*.json core.openapi.json
var Files embed.FS
