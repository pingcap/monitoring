//go:build !linux

package identity

// Container privilege preparation is Linux-specific; native development keeps
// the caller's identity and filesystem permissions.
func Prepare(watchDir, storeRoot, version string) error { return nil }
