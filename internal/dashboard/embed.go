//go:build dashboard

package dashboard

import "embed"

//go:embed all:static
var staticFS embed.FS
