// Package web embeds the operator console served by nacd at /ui/.
//
// The console is deliberately small: plain HTML, CSS and JavaScript with no
// build step and no third-party code, so it ships inside the nacd binary and
// works on an isolated management network. It is a client of the REST API
// (/api/v1) and holds no logic the API does not enforce.
package web

import "embed"

// UI holds the console's static files.
//
//go:embed ui/*
var UI embed.FS
