package tui

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrRuntimeDependenciesMissing = errors.New("runtime dependencies missing")
	ErrInvalidBackupName          = errors.New("invalid backup name")
	ErrHookExecutablePath         = errors.New("invalid hook executable path")
	ErrUnsupportedBootloader      = errors.New("unsupported bootloader")
	ErrUnsupportedPackageHook     = errors.New("unsupported package-manager hook")
	ErrBackupNotFound             = errors.New("backup not found")
	ErrBackupIncomplete           = errors.New("backup incomplete")
	ErrBackupNotActivated         = errors.New("backup not activated")
	ErrEFIMountUnavailable        = errors.New("EFI mount unavailable")
	ErrInsufficientSnapshotSpace  = errors.New("insufficient snapshot space")
	ErrInsufficientEFISpace       = errors.New("insufficient EFI space")
	ErrFreeSpaceCheckFailed       = errors.New("free space check failed")
	ErrRootModulesMissing         = errors.New("root modules missing")
	ErrArchivedModulesUnsafe      = errors.New("archived root modules require manual restore")
	ErrRequiredToolUnavailable    = errors.New("required tool unavailable")
	ErrCommandFailed              = errors.New("command failed")
	ErrSyncFailed                 = errors.New("sync failed")
	ErrSourceDirectoryMissing     = errors.New("source directory missing")
	ErrModuleArchiveExists        = errors.New("module archive already exists")
	ErrModuleArchiveFailed        = errors.New("module archive failed")
	ErrMountPointNotFound         = errors.New("mount point not found")
)

type RuntimeDependenciesError struct {
	Missing []string
}

func (e *RuntimeDependenciesError) Error() string {
	return fmt.Sprintf("bootrecov cannot start because required dependencies are missing:\n- %s", strings.Join(e.Missing, "\n- "))
}

func (e *RuntimeDependenciesError) Unwrap() error {
	return ErrRuntimeDependenciesMissing
}

type BackupNameError struct {
	Name string
}

func (e *BackupNameError) Error() string {
	if strings.TrimSpace(e.Name) == "" {
		return ErrInvalidBackupName.Error()
	}
	return fmt.Sprintf("%s: %q", ErrInvalidBackupName, e.Name)
}

func (e *BackupNameError) Unwrap() error {
	return ErrInvalidBackupName
}

type HookExecutablePathError struct {
	Path   string
	Reason string
}

func (e *HookExecutablePathError) Error() string {
	if e.Path == "" {
		return e.Reason
	}
	return fmt.Sprintf("%s: %q", e.Reason, e.Path)
}

func (e *HookExecutablePathError) Unwrap() error {
	return ErrHookExecutablePath
}

type EFIMountError struct {
	MountRoot  string
	MountPoint string
	Cause      error
}

func (e *EFIMountError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("unable to verify EFI mount %s: %v", e.MountRoot, e.Cause)
	}
	return fmt.Sprintf("EFI mount %s is not mounted (nearest mount point: %s)", e.MountRoot, e.MountPoint)
}

func (e *EFIMountError) Unwrap() error {
	if e.Cause == nil {
		return ErrEFIMountUnavailable
	}
	return errors.Join(ErrEFIMountUnavailable, e.Cause)
}

func IsInsufficientSpaceError(err error) bool {
	return errors.Is(err, ErrInsufficientSnapshotSpace) || errors.Is(err, ErrInsufficientEFISpace)
}
