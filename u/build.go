package u

// BuildVersion is the source version this binary was built from, injected at
// build time via -ldflags "-X aipmc/u.BuildVersion=<git short sha>".
// It is written into every [BOOT] log line so each log segment can be mapped
// back to the exact commit that produced it.
var BuildVersion = "dev"
