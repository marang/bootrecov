package tui

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func grubInitrdArgs(backupPath string, microcodes []string, initramfs string) string {
	args := make([]string, 0, len(microcodes)+1)
	for _, mc := range microcodes {
		args = append(args, filepath.ToSlash(filepath.Join(backupPath, mc)))
	}
	args = append(args, filepath.ToSlash(filepath.Join(backupPath, initramfs)))
	return strings.Join(args, " ")
}

func grubVisiblePath(hostPath string) string {
	clean := filepath.Clean(hostPath)
	if mountPoint, err := findMountPoint(clean); err == nil && mountPoint != "" {
		if rel, relErr := filepath.Rel(mountPoint, clean); relErr == nil {
			if rel == "." {
				return "/"
			}
			return "/" + filepath.ToSlash(rel)
		}
	}

	for _, mountRoot := range []string{filepath.Clean(EfiDir), filepath.Clean(BootDir)} {
		if clean == mountRoot {
			return "/"
		}
		prefix := mountRoot + string(os.PathSeparator)
		if strings.HasPrefix(clean, prefix) {
			return "/" + filepath.ToSlash(strings.TrimPrefix(clean, prefix))
		}
	}

	return filepath.ToSlash(clean)
}

// AddGrubEntry adds one GRUB menuentry for the EFI copy of a fully synced backup.
func AddGrubEntry(b BootBackup) error {
	if err := ensureSupportedBootloader(); err != nil {
		return err
	}
	name := b.Name
	if name == "" {
		name = filepath.Base(filepath.Clean(b.Path))
	}
	name = strings.TrimSpace(name)
	if err := validateBackupName(name); err != nil {
		return err
	}

	canonical := buildBackupFromName(name)
	refreshBackupCompleteness(&canonical)
	if !canonical.HasSnapshot || !canonical.HasEFI || !canonical.InSync {
		return fmt.Errorf("%w: backup %q is not activated in EFI (press 'g' in TUI to activate)", ErrBackupNotActivated, name)
	}
	if !canonical.HasKernel || !canonical.HasInitramfs {
		return fmt.Errorf("%w: backup %q is incomplete", ErrBackupIncomplete, canonical.Path)
	}
	if err := validateRootModuleCompatibility(canonical); err != nil {
		return err
	}
	if err := ensureEFIMountAvailable(); err != nil {
		return err
	}

	displayPath := canonical.EFIPath
	grubPath := grubVisiblePath(canonical.EFIPath)
	id := backupIDForName(name)
	exists, err := grubEntryExistsByID(id)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	if err := ensureGrubFile(); err != nil {
		return err
	}
	original, err := os.ReadFile(GrubCustom)
	if err != nil {
		return err
	}
	st, err := os.Stat(GrubCustom)
	if err != nil {
		return err
	}
	perm := st.Mode().Perm() | 0o111

	cmdline := currentKernelCmdline()
	if !strings.Contains(cmdline, kernelCmdlineMarker) {
		cmdline = strings.TrimSpace(cmdline + " " + kernelCmdlineMarker + id)
	}
	entry := fmt.Sprintf("cat <<'EOF'\nmenuentry 'Bootrecov %s' --id %s {\n"+
		"    search --file --set=root %s/%s\n"+
		"    linux %s/%s %s\n"+
		"    initrd %s\n"+
		"}\nEOF\n",
		displayPath, id,
		grubPath, canonical.KernelImage,
		grubPath, canonical.KernelImage, cmdline,
		grubInitrdArgs(grubPath, canonical.MicrocodeImages, canonical.InitramfsImage))

	if err := os.WriteFile(GrubCustom, append(append([]byte{}, original...), []byte(entry)...), perm); err != nil {
		return err
	}
	if err := updateGrubConfig(); err != nil {
		_ = os.WriteFile(GrubCustom, original, perm)
		return err
	}
	return nil
}

func grubEntryExistsByID(id string) (bool, error) {
	entries, err := ListGrubEntries()
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func currentKernelCmdline() string {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return "rw"
	}
	fields := strings.Fields(string(data))
	filtered := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "BOOT_IMAGE=") || strings.HasPrefix(f, "initrd=") {
			continue
		}
		filtered = append(filtered, f)
	}
	if len(filtered) == 0 {
		return "rw"
	}
	return strings.Join(filtered, " ")
}

func ensureGrubFile() error {
	data, err := os.ReadFile(GrubCustom)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(GrubCustom, []byte(grubHeader), 0o755)
		}
		return err
	}
	if !bytes.HasPrefix(data, []byte(grubHeader)) {
		st, statErr := os.Stat(GrubCustom)
		if statErr != nil {
			return statErr
		}
		perm := st.Mode().Perm() | 0o111
		return os.WriteFile(GrubCustom, append([]byte(grubHeader), data...), perm)
	}
	return nil
}

// RemoveGrubEntry removes the entry block matching the GRUB id.
func RemoveGrubEntry(id string) error {
	if err := ensureSupportedBootloader(); err != nil {
		return err
	}
	data, err := os.ReadFile(GrubCustom)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	st, err := os.Stat(GrubCustom)
	if err != nil {
		return err
	}
	perm := st.Mode().Perm() | 0o111

	lines := strings.Split(string(data), "\n")
	var out []string
	skip := false
	for _, l := range lines {
		if skip {
			if strings.TrimSpace(l) == "EOF" {
				skip = false
			}
			continue
		}
		if strings.Contains(l, id) {
			if len(out) > 0 && strings.HasPrefix(strings.TrimSpace(out[len(out)-1]), "cat <<'EOF'") {
				out = out[:len(out)-1]
			}
			skip = true
			continue
		}
		out = append(out, l)
	}
	if err := os.WriteFile(GrubCustom, []byte(strings.Join(out, "\n")), perm); err != nil {
		return err
	}
	if err := updateGrubConfig(); err != nil {
		_ = os.WriteFile(GrubCustom, data, perm)
		return err
	}
	return nil
}

// ListGrubEntries parses GrubCustom and returns bootrecov entries.
func ListGrubEntries() ([]GrubEntry, error) {
	if err := ensureSupportedBootloader(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(GrubCustom)
	if err != nil {
		if os.IsNotExist(err) {
			return []GrubEntry{}, nil
		}
		return nil, err
	}
	var entries []GrubEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if entry, ok := parseBootrecovMenuentry(line); ok {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func parseBootrecovMenuentry(line string) (GrubEntry, bool) {
	const prefix = "menuentry 'Bootrecov "
	const middle = "' --id "
	if !strings.HasPrefix(line, prefix) {
		return GrubEntry{}, false
	}
	rest := strings.TrimPrefix(line, prefix)
	titleEnd := strings.Index(rest, middle)
	if titleEnd == -1 {
		return GrubEntry{}, false
	}
	backupPath := rest[:titleEnd]
	idPart := rest[titleEnd+len(middle):]
	idEnd := strings.Index(idPart, " {")
	if idEnd == -1 {
		return GrubEntry{}, false
	}
	id := strings.TrimSpace(idPart[:idEnd])
	if id == "" || !strings.HasPrefix(id, "bootrecov-") {
		return GrubEntry{}, false
	}
	return GrubEntry{
		ID:         id,
		BackupPath: backupPath,
		Name:       filepath.Base(backupPath),
	}, true
}

func removeStaleGrubEntries(backups []BootBackup, preserveByName map[string]struct{}) error {
	valid := map[string]string{}
	for _, b := range backups {
		if _, keep := preserveByName[b.Name]; keep {
			valid[backupIDForName(b.Name)] = b.EFIPath
			continue
		}
		if IsBootReady(b) {
			valid[backupIDForName(b.Name)] = b.EFIPath
		}
	}
	entries, err := ListGrubEntries()
	if err != nil {
		return err
	}
	for _, e := range entries {
		expectedPath, ok := valid[e.ID]
		if !ok || e.BackupPath != expectedPath {
			if err := RemoveGrubEntry(e.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func markGrubFlags(backups []BootBackup, entries []GrubEntry) {
	ids := map[string]struct{}{}
	for _, e := range entries {
		ids[e.ID] = struct{}{}
	}
	for i := range backups {
		_, ok := ids[backupIDForName(backups[i].Name)]
		backups[i].GrubEntryExists = ok
	}
}

func backupIDForName(name string) string {
	return backupID(filepath.Join(EfiDir, strings.TrimSpace(name)))
}

func RecoveryCommands(name string) (string, error) {
	if err := ensureSupportedBootloader(); err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if err := validateBackupName(name); err != nil {
		return "", err
	}
	canonical := buildBackupFromName(name)
	refreshBackupCompleteness(&canonical)
	if !canonical.HasSnapshot {
		return "", fmt.Errorf("%w: %q", ErrBackupNotFound, name)
	}
	if !canonical.HasEFI {
		return "", fmt.Errorf("%w: snapshot %q is not activated in EFI (press 'g' in TUI to activate)", ErrBackupNotActivated, name)
	}
	if !canonical.HasKernel || !canonical.HasInitramfs {
		return "", fmt.Errorf("%w: snapshot %q is incomplete", ErrBackupIncomplete, name)
	}
	if err := validateRootModuleCompatibility(canonical); err != nil {
		return "", err
	}

	grubPath := grubVisiblePath(canonical.EFIPath)
	id := backupIDForName(canonical.Name)
	cmdline := currentKernelCmdline()
	if !strings.Contains(cmdline, kernelCmdlineMarker) {
		cmdline = strings.TrimSpace(cmdline + " " + kernelCmdlineMarker + id)
	}
	return strings.Join([]string{
		fmt.Sprintf("search --file --set=root %s/%s", grubPath, canonical.KernelImage),
		fmt.Sprintf("linux %s/%s %s", grubPath, canonical.KernelImage, cmdline),
		fmt.Sprintf("initrd %s", grubInitrdArgs(grubPath, canonical.MicrocodeImages, canonical.InitramfsImage)),
		"boot",
	}, "\n"), nil
}

func updateGrubConfig() error {
	if !AutoUpdateGrub {
		return nil
	}
	if strings.TrimSpace(GrubMkconfig) == "" {
		return fmt.Errorf("%w: grub config regeneration is enabled but grub-mkconfig is not configured", ErrRequiredToolUnavailable)
	}
	if strings.TrimSpace(GrubCfgOutput) == "" {
		return fmt.Errorf("%w: grub config regeneration is enabled but grub.cfg output path is not configured", ErrRequiredToolUnavailable)
	}
	if _, err := exec.LookPath(GrubMkconfig); err != nil {
		return fmt.Errorf("%w: grub config regeneration failed: %s not found in PATH", ErrRequiredToolUnavailable, GrubMkconfig)
	}
	cmd := exec.Command(GrubMkconfig, "-o", GrubCfgOutput)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: grub config regeneration: %w: %s", ErrCommandFailed, err, strings.TrimSpace(string(out)))
	}
	return nil
}
