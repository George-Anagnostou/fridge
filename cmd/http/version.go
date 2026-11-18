package main

// Commit is the git SHA baked in at build time with
// -ldflags "-X main.Commit=<sha>". It is "unknown" for plain local builds.
var Commit = "unknown"
