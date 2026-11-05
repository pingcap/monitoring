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
		return checkPaths(watchDir, storeRoot, version, false)
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
	// Kubernetes may explicitly set runAsGroup independently of runAsUser.
	if os.Getgid() != 0 {
		gid = os.Getgid()
	}
	if err := preparePaths(watchDir, storeRoot, version, uid); err != nil {
		return err
	}
	return dropAndExec(uid, gid)
}

func preparePaths(watchDir, storeRoot, version string, uid int) error {
	if version != "" && (version != filepath.Base(version) || version == "." || version == "..") {
		return fmt.Errorf("invalid rules version %q", version)
	}
	if err := prepareDirectory(watchDir, false, true, uid); err != nil {
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
	if err := prepareDirectory(parent, true, false, uid); err != nil {
		return err
	}
	return prepareDirectory(filepath.Join(parent, version), true, true, uid)
}

// Open each component with NOFOLLOW, retaining directory descriptors so renames
// cannot redirect ownership changes to a symlink target.
func openDirectory(path string, create bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) == "/" {
		return nil, fmt.Errorf("rules path must be an absolute non-root directory: %q", path)
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return nil, fmt.Errorf("rules path must not contain ..: %q", path)
		}
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

func setOwnerMode(file *os.File, uid int, directory bool) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return err
	}
	if !directory && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1) {
		return fmt.Errorf("rule must be a regular file with one link: %s", file.Name())
	}
	ownershipChanged := int(st.Uid) != uid
	if ownershipChanged {
		if err := file.Chown(uid, -1); err != nil {
			return err
		}
	}
	mode := os.FileMode(st.Mode&0644) | 0600
	if directory {
		// Retain existing reader/writer groups and let atomic replacements inherit
		// that group, without granting the service additional group memberships.
		mode = os.FileMode(st.Mode&0075) | 0700 | os.ModeSetgid
		if st.Mode&unix.S_ISVTX != 0 {
			mode |= os.ModeSticky
		}
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Mode()&(os.ModePerm|os.ModeSetgid|os.ModeSetuid|os.ModeSticky) == mode {
		return nil
	}
	return file.Chmod(mode)
}

func prepareDirectory(path string, create, rules bool, uid int) error {
	dir, err := openDirectory(path, create)
	if err != nil {
		return err
	}
	defer dir.Close()
	readOnly, err := readOnlyDirectory(dir)
	if err != nil {
		return err
	}
	if readOnly {
		return nil
	}
	if err := setOwnerMode(dir, uid, true); err != nil {
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
		err = setOwnerMode(file, uid, false)
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
	// Keep groups already granted by the Pod, including a valid fsGroup 0.
	// Group 0 does not grant UID 0 or capabilities; never infer new groups from files.
	groups, err := os.Getgroups()
	if err != nil {
		return err
	}
	if err := syscall.Setgroups(groups); err != nil {
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

// Rechecked after exec under the actual serving identity, before listening.
// This catches inaccessible PVC roots instead of failing after an active save.
// CheckWritable is called only when an administrator can edit rules. Watch-only
// and viewer-only services do not need ownership suitable for atomic saves.
func CheckWritable(watchDir, storeRoot, version string) error {
	return checkPaths(watchDir, storeRoot, version, true)
}

func checkPaths(watchDir, storeRoot, version string, writable bool) error {
	paths := []string{watchDir}
	if version != "" {
		paths = append(paths, filepath.Join(storeRoot, "latest-rules"), filepath.Join(storeRoot, "latest-rules", version))
	}
	for i, path := range paths {
		dir, err := openDirectory(path, i > 0)
		if err != nil {
			return fmt.Errorf("access rules as serving user: %w", err)
		}
		access := uint32(unix.R_OK | unix.X_OK)
		readOnly, err := readOnlyDirectory(dir)
		if err != nil {
			dir.Close()
			return err
		}
		if writable && i != 1 && !readOnly {
			access |= unix.W_OK
		}
		if err := unix.Faccessat(int(dir.Fd()), ".", access, unix.AT_EACCESS); err != nil {
			dir.Close()
			return fmt.Errorf("serving identity cannot access rules directory %s: %w", path, err)
		}
		if writable && i != 1 {
			if err := checkReplacementGroups(dir); err != nil {
				dir.Close()
				return err
			}
		}
		dir.Close()
	}
	return nil
}

// Atomic saves preserve file groups. A non-root service can restore a different
// group only when Kubernetes has explicitly granted that group to the process.
func checkReplacementGroups(dir *os.File) error {
	readOnly, err := readOnlyDirectory(dir)
	if err != nil {
		return err
	}
	if readOnly {
		return nil
	} // Watch-only read-only mounts need no metadata writes.
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	inherited := os.Getegid()
	if info.Mode()&os.ModeSetgid != 0 {
		inherited = int(info.Sys().(*syscall.Stat_t).Gid)
	}
	allowed, err := os.Getgroups()
	if err != nil {
		return err
	}
	allowed = append(allowed, os.Getegid(), inherited)
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		if filepath.Ext(name) != ".yml" {
			continue
		}
		var st unix.Stat_t
		if err = unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFREG {
			continue
		}
		if st.Uid != uint32(os.Geteuid()) {
			return fmt.Errorf("rule %s owner %d cannot be preserved by serving UID %d; prepare ownership for the configured runtime identity", filepath.Join(dir.Name(), name), st.Uid, os.Geteuid())
		}
		ok := false
		for _, group := range allowed {
			if uint32(group) == st.Gid {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("rule %s group %d cannot be preserved by serving identity; align the rule directory group or grant it with pod supplementalGroups", filepath.Join(dir.Name(), name), st.Gid)
		}
	}
	return nil
}

func readOnlyDirectory(dir *os.File) (bool, error) {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(dir.Fd()), &stat); err != nil {
		return false, err
	}
	return stat.Flags&unix.ST_RDONLY != 0, nil
}
