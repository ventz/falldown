//go:build !js

package game

// DefaultStore keeps settings in DefaultStorePath.
func DefaultStore() Store { return NewFileStore(DefaultStorePath()) }
