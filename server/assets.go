package server

import _ "embed"

//go:embed app.js
var appJS string

//go:embed page.css
var pageCSS string
