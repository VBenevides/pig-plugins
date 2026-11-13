package distribution

import pigplugins "github.com/VBenevides/pig-plugins"

// PluginsVersion returns the distribution version embedded in this executable.
func PluginsVersion() string { return pigplugins.Version() }
