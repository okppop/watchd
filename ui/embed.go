package ui

import "embed"

// Assets contains the production React application built with npm run build.
//
//go:embed all:dist
var Assets embed.FS
