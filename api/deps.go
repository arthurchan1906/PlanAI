package api

import "aipmc/app"

// Deps holds shared application services for HTTP handlers.
type Deps struct {
	App *app.App
}
