//go:build unix

package bizlogic

import (
	"fmt"
	"os"
	"syscall"
)

func preserveConfigOwnership(f *os.File, original os.FileInfo) error {
	want, ok := original.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine original ownership")
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	got, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine staged file ownership")
	}
	uid, gid := -1, -1
	if got.Uid != want.Uid {
		uid = int(want.Uid)
	}
	if got.Gid != want.Gid {
		gid = int(want.Gid)
	}
	if uid == -1 && gid == -1 {
		return nil
	}
	return f.Chown(uid, gid)
}
