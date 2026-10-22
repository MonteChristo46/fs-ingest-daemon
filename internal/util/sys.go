package util

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
)

// IsAdmin checks if the current process has administrator or root privileges.
func IsAdmin() bool {
	if runtime.GOOS == "windows" {
		// On Windows, checking for access to a physical drive is a common way to check for admin rights
		_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
		return err == nil
	}
	u, err := user.Current()
	if err != nil {
		return false
	}
	return u.Uid == "0"
}

// GetExecutableDir returns the absolute path to the directory containing the current executable,
// correctly resolving symlinks to find the actual installation location.
func GetExecutableDir() string {
	ex, err := os.Executable()
	if err != nil {
		return "."
	}
	// Resolve symlinks to find the real binary path (e.g., /opt/hunt/hunt instead of /usr/local/bin/hunt)
	realPath, err := filepath.EvalSymlinks(ex)
	if err != nil {
		return filepath.Dir(ex)
	}
	return filepath.Dir(realPath)
}

// GetRealUserHome attempts to find the actual user's home directory,
// even if the program is running under sudo or as Administrator.
func GetRealUserHome() string {
	// If running under sudo (Linux/macOS), SUDO_USER gives the original user
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser != "" {
		u, err := user.Lookup(sudoUser)
		if err == nil {
			return u.HomeDir
		}
	}

	// Otherwise, current user (On Windows, Admin elevation keeps the same user profile typically)
	u, err := user.Current()
	if err == nil {
		return u.HomeDir
	}

	// Fallback
	home, _ := os.UserHomeDir()
	return home
}

// EnsureUserOwnership attempts to give ownership or read/write permissions
// of the given path to the actual user, bypassing root/Admin restrictions.
// It applies the changes recursively.
func EnsureUserOwnership(targetPath string) error {
	if runtime.GOOS == "windows" {
		// On Windows, grant the "Users" group read/write/execute permissions.
		cmd := exec.Command("icacls", targetPath, "/grant", "Users:(OI)(CI)F", "/T")
		return cmd.Run()
	}

	// Linux/macOS
	sudoUid := os.Getenv("SUDO_UID")
	sudoGid := os.Getenv("SUDO_GID")

	if sudoUid != "" && sudoGid != "" {
		uid, err1 := strconv.Atoi(sudoUid)
		gid, err2 := strconv.Atoi(sudoGid)

		if err1 == nil && err2 == nil {
			// Walk up the path to the real user's home directory to fix any root-owned parent directories
			// created by MkdirAll. We only go as far as the user's home dir.
			homeDir := GetRealUserHome()
			current := targetPath
			for current != "" && current != "/" && current != "." && current != homeDir {
				_ = os.Chown(current, uid, gid)
				current = filepath.Dir(current)
			}

			// Change ownership of the target directory and all its contents
			return filepath.Walk(targetPath, func(p string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				return os.Chown(p, uid, gid)
			})
		}
	}

	// Fallback to chmod 777/666 if we can't figure out the SUDO user (e.g. running as root directly)
	// Only apply this to targetPath itself and its children, not parents.
	return filepath.Walk(targetPath, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.Chmod(p, 0777)
		}
		return os.Chmod(p, 0666)
	})
}
