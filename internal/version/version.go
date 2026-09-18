package version

// Version is the running build. Release packaging overwrites it with
// -ldflags "-X grokmcp/internal/version.Version=<tag without v>".
var Version = "dev"
