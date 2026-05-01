package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	PlatformArch    = "arch"
	PlatformUbuntu  = "ubuntu"
	PlatformDebian  = "debian"
	PlatformUnknown = "unknown"

	BootloaderGRUB        = "grub"
	BootloaderSystemdBoot = "systemd-boot"
	BootloaderUnknown     = "unknown"
)

const (
	defaultBootDir       = "/boot"
	defaultESPRoot       = "/boot/efi"
	defaultEFIMirrorDir  = "/boot/efi/bootrecov-snapshots"
	defaultGrubCfgOutput = "/boot/grub/grub.cfg"
)

type SystemLayout struct {
	BootDir        string
	ESPRoot        string
	EFIMirrorDir   string
	SnapshotDir    string
	RootModulesDir string
	GrubCustom     string
	GrubCfgOutput  string
	PacmanHookPath string
}

type RuntimeEnvironment struct {
	PlatformID          string
	PlatformName        string
	BootloaderID        string
	BootloaderName      string
	BootloaderSupported bool
	HookSupported       bool
	Layout              SystemLayout
	Warnings            []string
}

var (
	PlatformOverride   string
	BootloaderOverride string
	OSReleasePath      = "/etc/os-release"
	GrubDefaultPath    = "/etc/default/grub"

	activePlatformID     = PlatformArch
	activePlatformName   = "Arch Linux"
	activeHookSupported  = true
	activeBootloaderID   = BootloaderGRUB
	activeBootloaderName = "GRUB"
	activeWarnings       []string
)

func ApplyEnvironmentOverridesFromEnv() {
	if v := strings.TrimSpace(os.Getenv("BOOTRECOV_BACKUP_PROFILE")); v != "" {
		BackupProfile = v
	}
	if v := strings.TrimSpace(os.Getenv("BOOTRECOV_PLATFORM")); v != "" {
		PlatformOverride = normalizePlatformID(v)
	}
	if v := strings.TrimSpace(os.Getenv("BOOTRECOV_BOOTLOADER")); v != "" {
		BootloaderOverride = normalizeBootloaderID(v)
	}
	if v := strings.TrimSpace(os.Getenv("BOOTRECOV_BOOT_DIR")); v != "" {
		BootDir = filepath.Clean(v)
	}
	if v := strings.TrimSpace(os.Getenv("BOOTRECOV_ESP_DIR")); v != "" {
		EfiDir = filepath.Join(filepath.Clean(v), "bootrecov-snapshots")
	}
	if v := strings.TrimSpace(os.Getenv("BOOTRECOV_EFI_MIRROR_DIR")); v != "" {
		EfiDir = filepath.Clean(v)
	}
}

func ConfigureDetectedEnvironment() RuntimeEnvironment {
	applyDetectedLayoutDefaults()
	activeWarnings = nil

	platformID, platformName := detectPlatform()
	bootloaderID, bootloaderName := detectBootloader()

	activePlatformID = platformID
	activePlatformName = platformName
	activeHookSupported = platformSupportsHook(platformID)
	activeBootloaderID = bootloaderID
	activeBootloaderName = bootloaderName

	applyPlatformDefaults(platformID)
	applyBootloaderDefaults(bootloaderID)

	info := CurrentRuntimeEnvironment()
	if !info.BootloaderSupported {
		info.Warnings = append(info.Warnings, fmt.Sprintf("bootloader %q is detected but not supported yet", info.BootloaderID))
	}
	if !info.HookSupported {
		info.Warnings = append(info.Warnings, fmt.Sprintf("package-manager hooks are not implemented for platform %q", info.PlatformID))
	}
	sort.Strings(info.Warnings)
	activeWarnings = append([]string{}, info.Warnings...)
	return info
}

func CurrentRuntimeEnvironment() RuntimeEnvironment {
	return RuntimeEnvironment{
		PlatformID:          currentPlatformID(),
		PlatformName:        activePlatformName,
		BootloaderID:        currentBootloaderID(),
		BootloaderName:      activeBootloaderName,
		BootloaderSupported: bootloaderSupported(currentBootloaderID()),
		HookSupported:       activeHookSupported,
		Layout:              currentSystemLayout(),
		Warnings:            append([]string{}, activeWarnings...),
	}
}

func currentSystemLayout() SystemLayout {
	return SystemLayout{
		BootDir:        BootDir,
		ESPRoot:        filepath.Dir(EfiDir),
		EFIMirrorDir:   EfiDir,
		SnapshotDir:    SnapshotDir,
		RootModulesDir: RootModulesDir,
		GrubCustom:     GrubCustom,
		GrubCfgOutput:  GrubCfgOutput,
		PacmanHookPath: PacmanHookPath,
	}
}

func currentPlatformID() string {
	if activePlatformID == "" {
		return PlatformArch
	}
	return activePlatformID
}

func currentBootloaderID() string {
	if activeBootloaderID == "" {
		return BootloaderGRUB
	}
	return activeBootloaderID
}

func ensureSupportedBootloader() error {
	if bootloaderSupported(currentBootloaderID()) {
		return nil
	}
	return fmt.Errorf("bootloader %q is not supported yet", currentBootloaderID())
}

func ensurePlatformHookSupported() error {
	if activeHookSupported {
		return nil
	}
	if currentPlatformID() == PlatformUbuntu || currentPlatformID() == PlatformDebian {
		return fmt.Errorf("package-manager hook install is not implemented for %s yet; apt/dpkg hook support is planned", currentPlatformID())
	}
	return fmt.Errorf("package-manager hook install is not supported for platform %q", currentPlatformID())
}

func detectPlatform() (string, string) {
	if PlatformOverride != "" {
		return platformNameForID(PlatformOverride)
	}
	data, err := os.ReadFile(OSReleasePath)
	if err != nil {
		return PlatformUnknown, "Unknown Linux"
	}
	return detectPlatformFromOSRelease(parseOSRelease(data))
}

func detectPlatformFromOSRelease(values map[string]string) (string, string) {
	id := normalizePlatformID(values["ID"])
	idLike := strings.Fields(strings.ToLower(values["ID_LIKE"]))
	if id == PlatformArch || containsString(idLike, PlatformArch) {
		return PlatformArch, valueOr(values["PRETTY_NAME"], "Arch Linux")
	}
	if id == PlatformUbuntu || containsString(idLike, PlatformUbuntu) {
		return PlatformUbuntu, valueOr(values["PRETTY_NAME"], "Ubuntu")
	}
	if id == PlatformDebian || containsString(idLike, PlatformDebian) {
		return PlatformDebian, valueOr(values["PRETTY_NAME"], "Debian")
	}
	if id == "" {
		return PlatformUnknown, valueOr(values["PRETTY_NAME"], "Unknown Linux")
	}
	return id, valueOr(values["PRETTY_NAME"], id)
}

func detectBootloader() (string, string) {
	if BootloaderOverride != "" {
		return bootloaderNameForID(BootloaderOverride)
	}
	hasSystemdBoot := systemdBootSignal()
	hasStrongGRUB := strongGRUBSignal()
	hasWeakGRUB := weakGRUBSignal()
	if hasSystemdBoot && hasStrongGRUB {
		return BootloaderUnknown, "Ambiguous bootloader"
	}
	if hasSystemdBoot {
		return BootloaderSystemdBoot, "systemd-boot"
	}
	if hasStrongGRUB || hasWeakGRUB {
		return BootloaderGRUB, "GRUB"
	}
	if _, err := os.Stat("/sys/firmware/efi"); err == nil {
		return BootloaderUnknown, "Unknown EFI bootloader"
	}
	return BootloaderGRUB, "GRUB"
}

func strongGRUBSignal() bool {
	return fileExists(GrubCustom) || fileExists(GrubCfgOutput)
}

func weakGRUBSignal() bool {
	return fileExists(GrubDefaultPath)
}

func systemdBootSignal() bool {
	espRoot := filepath.Dir(EfiDir)
	return dirExists(filepath.Join(espRoot, "loader", "entries")) || fileExists(filepath.Join(espRoot, "loader", "loader.conf"))
}

func applyDetectedLayoutDefaults() {
	if !envConfigured("BOOTRECOV_BOOT_DIR") && sameCleanPath(BootDir, defaultBootDir) {
		if detected := detectBootDir(); detected != "" {
			BootDir = detected
		}
	}
	if !envConfigured("BOOTRECOV_ESP_DIR") && !envConfigured("BOOTRECOV_EFI_MIRROR_DIR") && sameCleanPath(EfiDir, defaultEFIMirrorDir) {
		if detected := detectESPRoot(); detected != "" {
			EfiDir = filepath.Join(detected, "bootrecov-snapshots")
		}
	}
	if sameCleanPath(GrubCfgOutput, defaultGrubCfgOutput) {
		if detected := detectGrubCfgOutput(); detected != "" {
			GrubCfgOutput = detected
		}
	}
}

func detectBootDir() string {
	candidates := []string{BootDir}
	if entries, err := readMountInfo(); err == nil {
		for _, entry := range entries {
			candidates = append(candidates, entry.mountPoint)
		}
	}
	candidates = append(candidates, defaultBootDir, "/efi", defaultESPRoot)
	for _, candidate := range uniqueCleanPaths(candidates) {
		if bootDirLooksUsable(candidate) {
			return candidate
		}
	}
	return ""
}

func detectESPRoot() string {
	entries, err := readMountInfo()
	if err != nil {
		return ""
	}
	var candidates []string
	for _, entry := range entries {
		if espFSType(entry.fsType) {
			candidates = append(candidates, entry.mountPoint)
		}
	}
	candidates = uniqueCleanPaths(candidates)
	sort.SliceStable(candidates, func(i, j int) bool {
		return espMountScore(candidates[i]) < espMountScore(candidates[j])
	})
	for _, candidate := range candidates {
		if espRootLooksUsable(candidate) {
			return candidate
		}
	}
	return ""
}

func detectGrubCfgOutput() string {
	candidates := []string{
		filepath.Join(BootDir, "grub", "grub.cfg"),
		filepath.Join(BootDir, "grub2", "grub.cfg"),
		defaultGrubCfgOutput,
	}
	for _, candidate := range uniqueCleanPaths(candidates) {
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func bootDirLooksUsable(path string) bool {
	if !dirExists(path) {
		return false
	}
	for _, pattern := range []string{"vmlinuz*", "initrd.img*", "initramfs*.img"} {
		matches, err := filepath.Glob(filepath.Join(path, pattern))
		if err == nil && len(matches) > 0 {
			return true
		}
	}
	return dirExists(filepath.Join(path, "grub")) || dirExists(filepath.Join(path, "loader"))
}

func espFSType(fsType string) bool {
	switch strings.ToLower(strings.TrimSpace(fsType)) {
	case "vfat", "msdos", "exfat", "fat", "fat32":
		return true
	default:
		return false
	}
}

func espRootLooksUsable(path string) bool {
	if !dirExists(path) {
		return false
	}
	switch filepath.Clean(path) {
	case defaultESPRoot, "/efi", defaultBootDir:
		return true
	}
	return dirExists(filepath.Join(path, "EFI")) ||
		dirExists(filepath.Join(path, "loader", "entries")) ||
		fileExists(filepath.Join(path, "loader", "loader.conf"))
}

func espMountScore(path string) int {
	switch filepath.Clean(path) {
	case defaultESPRoot:
		return 0
	case "/efi":
		return 1
	case defaultBootDir:
		return 2
	}
	if strings.Contains(strings.ToLower(filepath.Base(path)), "efi") {
		return 3
	}
	return 4
}

func parseOSRelease(data []byte) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(val), `"'`)
	}
	return values
}

func applyPlatformDefaults(platformID string) {
	switch platformID {
	case PlatformArch:
		if PacmanHookPath == "" {
			PacmanHookPath = "/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook"
		}
		activeHookSupported = true
	case PlatformUbuntu, PlatformDebian:
		activeHookSupported = false
	default:
		activeHookSupported = false
	}
}

func applyBootloaderDefaults(bootloaderID string) {
	if bootloaderID != BootloaderGRUB {
		return
	}
	if GrubCustom == "" {
		GrubCustom = "/etc/grub.d/41_bootrecov_snapshots"
	}
	if GrubCfgOutput == "" {
		GrubCfgOutput = "/boot/grub/grub.cfg"
	}
	if GrubMkconfig == "" {
		GrubMkconfig = "grub-mkconfig"
	}
}

func platformSupportsHook(platformID string) bool {
	return platformID == PlatformArch
}

func bootloaderSupported(bootloaderID string) bool {
	return bootloaderID == "" || bootloaderID == BootloaderGRUB
}

func normalizePlatformID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	switch id {
	case "archlinux":
		return PlatformArch
	case "ubuntu", "debian", "arch":
		return id
	default:
		return id
	}
}

func normalizeBootloaderID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	switch id {
	case "systemd", "systemdboot", "sd-boot", "systemd-boot":
		return BootloaderSystemdBoot
	case "grub", "grub2":
		return BootloaderGRUB
	default:
		return id
	}
}

func platformNameForID(id string) (string, string) {
	switch id {
	case PlatformArch:
		return PlatformArch, "Arch Linux"
	case PlatformUbuntu:
		return PlatformUbuntu, "Ubuntu"
	case PlatformDebian:
		return PlatformDebian, "Debian"
	case "":
		return PlatformUnknown, "Unknown Linux"
	default:
		return id, id
	}
}

func bootloaderNameForID(id string) (string, string) {
	switch id {
	case BootloaderGRUB:
		return BootloaderGRUB, "GRUB"
	case BootloaderSystemdBoot:
		return BootloaderSystemdBoot, "systemd-boot"
	case "":
		return BootloaderUnknown, "Unknown bootloader"
	default:
		return id, id
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func envConfigured(key string) bool {
	return strings.TrimSpace(os.Getenv(key)) != ""
}

func sameCleanPath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func uniqueCleanPaths(paths []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}
