package adminui

import "embed"

// embeddedStatic holds the Vite build output (kernal/adminui/static/).
// Regenerate with: make -C kernal admin-ui
//
//go:embed all:static
var embeddedStatic embed.FS
