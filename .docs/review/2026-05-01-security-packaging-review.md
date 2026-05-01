# Security And Packaging Review

Date: 2026-05-01

## Findings

- [BLOCKING] Path traversal in snapshot names: `ActivateBackup` and `DeactivateBackup` accept the CLI name without the same validation as `DeleteBackup`. Through `filepath.Join(EfiDir, name)`, a value such as `../foo` can escape `/boot/efi/bootrecov-snapshots`; `DeactivateBackup` then follows with `os.RemoveAll(...)`. See `/tmp/bootrecov-upstream/internal/tui/backups.go:1100`, `/tmp/bootrecov-upstream/internal/tui/backups.go:1122`, `/tmp/bootrecov-upstream/internal/tui/backups.go:281`. Fix: one central `validateBackupName` / `resolveUnderRoot` function for all CLI paths, not just `DeleteBackup`.
- [MAJOR] EFI mount is not verified: `checkEFISpaceForSnapshot` uses `existingParent(EfiDir)`. If `/boot/efi` is not mounted, the tool can write into a normal directory under `/boot/efi` on the root filesystem and still generate GRUB entries. See `/tmp/bootrecov-upstream/internal/tui/backups.go:592`, `/tmp/bootrecov-upstream/internal/tui/backups.go:625`, `/tmp/bootrecov-upstream/internal/tui/backups.go:1109`. Fix: before activation, verify that `EfiDir` or a defined parent is actually a mountpoint and reachable from GRUB.
- [MAJOR] `rclone` flag detection misses real alias lines: `helpMentionsFlag` only detects lines that start directly with `--links`. In real help output this appears as `-l, --links ...`, so `--links` is not set. Symlinks are then not copied by `rclone sync`, which can create incomplete snapshots on symlink-based `/boot` layouts. See `/tmp/bootrecov-upstream/internal/tui/backups.go:1221`, `/tmp/bootrecov-upstream/internal/tui/backups.go:1244`, and the idealized test output in `/tmp/bootrecov-upstream/internal/tui/backups_test.go:364`. Fix: detect flags with regex or word boundaries anywhere in the flag column, or directly configure known stable flags.
- [MAJOR] AUR build does not include Go modules as sources: `PKGBUILD` builds directly with `go build`, but `source=...` contains only the upstream tarball. In a clean/offline build, Go must fetch modules from the network. See `/tmp/bootrecov-aur/PKGBUILD:11`, `/tmp/bootrecov-aur/PKGBUILD:16`. Fix: use proper Arch Go packaging, for example controlled module download in the appropriate build step with isolated `GOMODCACHE`, or vendored/reproducible sources.

## Tests

`go test ./...` and `go vet ./...` pass. The largest gaps are integration tests for real GRUB/EFI mount states, real `rclone sync --help` output, and malicious or invalid snapshot names.
