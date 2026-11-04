//go:build unix

package bizlogic

import "golang.org/x/sys/unix"

// A file replaced with a FIFO must not block before the descriptor type check.
const nonblockOpenFlag = unix.O_NONBLOCK
