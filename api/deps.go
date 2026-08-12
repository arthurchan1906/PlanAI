package api

import "aipmc/app"

// Deps holds shared application services for HTTP handlers.
type Deps struct {
	App         *app.App
	ProjectPath string // project root the web server is serving (agent launch cwd)
}
