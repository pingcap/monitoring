//go:build !unix

package bizlogic

import "os"

// These platforms do not expose Unix UID/GID ownership through FileInfo.
func preserveConfigOwnership(_ *os.File, _ os.FileInfo) error { return nil }
