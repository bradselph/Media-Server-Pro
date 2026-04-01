package mediaserver

import "media-server-pro/internal/logger"

// Option configures a Server during construction.
type Option func(*serverConfig)

// serverConfig holds the resolved configuration for server construction.
type serverConfig struct {
	configPath string
	logLevel   logger.Level
	version    string
	buildDate  string
	moduleSet  ModuleSet
	// onReady is called after all modules have started and the HTTP server is
	// about to begin accepting connections. Useful for integration tests or
	// programmatic consumers that need to know when the server is ready.
	onReady func(*Server)
}

func defaultServerConfig() *serverConfig {
	return &serverConfig{
		configPath: "config.json",
		logLevel:   logger.INFO,
		version:    "0.0.0-embedded",
		moduleSet:  AllModules,
	}
}

// WithConfigPath sets the path to the JSON configuration file.
// Defaults to "config.json".
func WithConfigPath(path string) Option {
	return func(c *serverConfig) {
		c.configPath = path
	}
}

// WithLogLevel sets the server log level.
// Defaults to logger.INFO.
func WithLogLevel(level logger.Level) Option {
	return func(c *serverConfig) {
		c.logLevel = level
	}
}

// WithVersion sets the version string reported by the /api/version endpoint.
func WithVersion(version string) Option {
	return func(c *serverConfig) {
		c.version = version
	}
}

// WithBuildDate sets the build date string reported by the /api/version endpoint.
func WithBuildDate(date string) Option {
	return func(c *serverConfig) {
		c.buildDate = date
	}
}

// WithModuleSet selects a predefined set of modules to register.
// Use CoreModules, StandardModules, or AllModules.
// Defaults to AllModules.
func WithModuleSet(set ModuleSet) Option {
	return func(c *serverConfig) {
		c.moduleSet = set
	}
}

// WithModules selects individual modules to register.
// The required dependency modules (database, auth, security) are always
// included even if not specified.
func WithModules(modules ...ModuleID) Option {
	return func(c *serverConfig) {
		c.moduleSet = ModuleSet(modules)
	}
}

// WithOnReady registers a callback that fires after all modules have started
// and the HTTP listener is bound.
func WithOnReady(fn func(*Server)) Option {
	return func(c *serverConfig) {
		c.onReady = fn
	}
}
