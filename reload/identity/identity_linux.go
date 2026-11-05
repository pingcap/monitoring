//go:build linux

// Package identity prepares legacy rule volumes before starting the service as non-root.
package identity

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Prepare runs before loading credentials, starting watchers or listening. An
// explicit non-root runtime identity is already authoritative and is left alone.
func Prepare(watchDir, storeRoot, version string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	account, err := user.Lookup("pingcap")
	if err != nil {
		return fmt.Errorf("find non-root image account: %w", err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil || uid <= 0 {
		return fmt.Errorf("invalid non-root UID %q", account.Uid)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil || gid <= 0 {
		return fmt.Errorf("invalid non-root GID %q", account.Gid)
	}
	if err := preparePaths(watchDir, storeRoot, version, uid, gid); err != nil {
		return err
	}
	return dropAndExec(uid, gid)
}

func preparePaths(watchDir, storeRoot, version string, uid, gid int) error {
	if version != "" && (version != filepath.Base(version) || version == "." || version == "..") {
		return fmt.Errorf("invalid rules version %q", version)
	}
	if err := prepareDirectory(watchDir, false, true, uid, gid); err != nil {
		return err
	}
	if version == "" {
		return nil
	}
	// The volume root must already exist. Only create and change dedicated rule directories.
	root, err := openDirectory(storeRoot, false)
	if err != nil {
		return err
	}
	root.Close()
	parent := filepath.Join(storeRoot, "latest-rules")
	if err := prepareDirectory(parent, true, false, uid, gid); err != nil {
		return err
	}
	return prepareDirectory(filepath.Join(parent, version), true, true, uid, gid)
}

// Open each component with NOFOLLOW, retaining directory descriptors so renames
// cannot redirect ownership changes to a symlink target.
func openDirectory(path string, create bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) == "/" {
		return nil, fmt.Errorf("rules path must be an absolute non-root directory: %q", path)
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(filepath.Clean(path)[1:], "/")
	for i, part := range parts {
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr == unix.ENOENT && create && i == len(parts)-1 {
			if err := unix.Mkdirat(fd, part, 0755); err != nil && err != unix.EEXIST {
				unix.Close(fd)
				return nil, err
			}
			next, openErr = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		unix.Close(fd)
		if openErr != nil {
			return nil, fmt.Errorf("open rules directory %q: %w", path, openErr)
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}

func setOwnerMode(file *os.File, uid, gid int, directory bool) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return err
	}
	if !directory && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1) {
		return fmt.Errorf("rule must be a regular file with one link: %s", file.Name())
	}
	if int(st.Uid) != uid || int(st.Gid) != gid {
		if err := file.Chown(uid, gid); err != nil {
			return err
		}
	}
	mode := os.FileMode(st.Mode&0644) | 0600
	if directory {
		mode = os.FileMode(st.Mode&0055) | 0700
	}
	return file.Chmod(mode)
}

func prepareDirectory(path string, create, rules bool, uid, gid int) error {
	dir, err := openDirectory(path, create)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := setOwnerMode(dir, uid, gid, true); err != nil {
		return fmt.Errorf("prepare %s: %w", path, err)
	}
	if !rules {
		return nil
	}
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		if filepath.Ext(name) != ".yml" {
			continue
		}
		fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("open rule %s: %w", name, err)
		}
		file := os.NewFile(uintptr(fd), filepath.Join(path, name))
		err = setOwnerMode(file, uid, gid, false)
		file.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func dropAndExec(uid, gid int) error {
	// Go's syscall credential wrappers update all runtime threads. Lock this
	// thread for the thread-local capability/prctl operations and subsequent exec.
	runtime.LockOSThread()
	if err := unix.Prctl(unix.PR_SET_KEEPCAPS, 0, 0, 0, 0); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	if err := syscall.Setgroups([]int{}); err != nil {
		return err
	}
	if err := syscall.Setgid(gid); err != nil {
		return err
	}
	if err := syscall.Setuid(uid); err != nil {
		return err
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{}
	if err := unix.Capset(&header, &data[0]); err != nil {
		return err
	}
	// Replace the entire process: no root supervisor, bootstrap marker, extra
	// arguments, or inherited rule descriptors remain in the serving process.
	return syscall.Exec("/proc/self/exe", os.Args, os.Environ())
}
