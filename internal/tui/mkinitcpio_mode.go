package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type mkinitcpioMode struct {
	name   string
	reason string
	hooks  []string
}

var mkinitcpioAssignment = regexp.MustCompile(`^([A-Za-z_][A-Za-z_0-9]*)=(.*)$`)
var mkinitcpioLiteralWord = regexp.MustCompile(`^[A-Za-z0-9_./%+:-]+$`)

// mkinitcpio sources shell files. Inspect only literal values: evaluating those
// files in doctor or before installation would execute arbitrary host commands.
func inspectMkinitcpioMode(configPath string) mkinitcpioMode {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return mkinitcpioMode{reason: fmt.Sprintf("mkinitcpio config %s: %v", configPath, err)}
	}
	hooks, found, err := literalHooks(data)
	if err != nil || !found {
		return mkinitcpioMode{reason: fmt.Sprintf("mkinitcpio config %s has no unambiguous literal HOOKS=(...) assignment", configPath)}
	}
	mode := mkinitcpioMode{name: "busybox", hooks: hooks}
	if containsHook(hooks, "systemd") {
		mode.name = "systemd"
		mode.reason = "systemd initramfs does not run Bootrecov's BusyBox runtime hook"
	}

	presetDir := os.Getenv("MKINITCPIO_PRESETS")
	if presetDir == "" {
		presetDir = "/etc/mkinitcpio.d"
	}
	presets, err := mkinitcpioConfigFiles(presetDir, ".preset")
	if err != nil {
		return mkinitcpioMode{reason: fmt.Sprintf("cannot inspect mkinitcpio presets: %v", err)}
	}
	usesDropIns := len(presets) == 0
	for _, path := range presets {
		data, err := os.ReadFile(path)
		if err != nil {
			return mkinitcpioMode{reason: fmt.Sprintf("mkinitcpio preset %s: %v", path, err)}
		}
		presetUsesDropIns, reason := inspectMkinitcpioPreset(data, path, configPath)
		if reason != "" {
			return mkinitcpioMode{reason: reason}
		}
		usesDropIns = usesDropIns || presetUsesDropIns
	}
	if !usesDropIns {
		return mode
	}
	dropIns, err := mkinitcpioConfigFiles(configPath+".d", ".conf")
	if err != nil {
		return mkinitcpioMode{reason: fmt.Sprintf("cannot inspect mkinitcpio drop-ins: %v", err)}
	}
	for _, path := range dropIns {
		data, err := os.ReadFile(path)
		if err != nil {
			return mkinitcpioMode{reason: fmt.Sprintf("mkinitcpio drop-in %s: %v", path, err)}
		}
		dropHooks, found, err := literalHooks(data)
		if err != nil {
			return mkinitcpioMode{reason: fmt.Sprintf("mkinitcpio drop-in %s has ambiguous HOOKS: %v", path, err)}
		}
		if found {
			mode.hooks = dropHooks
			if containsHook(dropHooks, "systemd") {
				mode.name = "systemd"
				mode.reason = fmt.Sprintf("systemd initramfs selected by drop-in %s", path)
			} else {
				mode.name = "busybox"
				mode.reason = fmt.Sprintf("drop-in %s overrides HOOKS in %s; Bootrecov cannot enable its runtime hook there", path, configPath)
			}
		}
	}

	return mode
}

func mkinitcpioConfigFiles(dir, suffix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), suffix) {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	return paths, nil
}

func literalHooks(data []byte) ([]string, bool, error) {
	var hooks []string
	found := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		match := mkinitcpioAssignment.FindStringSubmatch(line)
		if len(match) != 3 || (match[1] != "HOOKS" && strings.Contains(line, "HOOKS")) {
			return nil, false, fmt.Errorf("dynamic shell syntax or HOOKS modification")
		}
		if strings.ContainsAny(match[2], ";`") || strings.Contains(match[2], "&&") || strings.Contains(match[2], "||") {
			return nil, false, fmt.Errorf("shell commands in mkinitcpio assignment")
		}
		if match[1] != "HOOKS" {
			continue
		}
		if found {
			return nil, false, fmt.Errorf("multiple HOOKS assignments")
		}
		value := strings.TrimSpace(match[2])
		if !strings.HasPrefix(value, "(") {
			return nil, false, fmt.Errorf("non-array HOOKS assignment")
		}
		close := strings.Index(value, ")")
		trailing := ""
		if close >= 0 {
			trailing = strings.TrimSpace(value[close+1:])
		}
		if close < 0 || (trailing != "" && !strings.HasPrefix(trailing, "#")) {
			return nil, false, fmt.Errorf("non-literal HOOKS assignment")
		}
		words, ok := literalShellWords(value[1:close])
		if !ok || len(words) == 0 {
			return nil, false, fmt.Errorf("non-literal HOOKS values")
		}
		hooks, found = words, true
	}
	return hooks, found, nil
}

func literalShellWords(value string) ([]string, bool) {
	words := strings.Fields(value)
	for i, word := range words {
		word = strings.Trim(word, `"'`)
		if !mkinitcpioLiteralWord.MatchString(word) {
			return nil, false
		}
		words[i] = word
	}
	return words, true
}

func containsHook(hooks []string, name string) bool {
	for _, hook := range hooks {
		if hook == name {
			return true
		}
	}
	return false
}

func inspectMkinitcpioPreset(data []byte, presetPath, configPath string) (bool, string) {
	assignments := make(map[string]string)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		match := mkinitcpioAssignment.FindStringSubmatch(line)
		if len(match) != 3 {
			return false, fmt.Sprintf("mkinitcpio preset %s has dynamic shell syntax", presetPath)
		}
		if strings.ContainsAny(match[2], ";`") || strings.Contains(match[2], "&&") || strings.Contains(match[2], "||") {
			return false, fmt.Sprintf("mkinitcpio preset %s has shell commands in an assignment", presetPath)
		}
		assignments[match[1]] = strings.TrimSpace(match[2])
	}
	presetValue, ok := assignments["PRESETS"]
	if !ok || !strings.HasPrefix(presetValue, "(") || !strings.HasSuffix(presetValue, ")") {
		return false, fmt.Sprintf("mkinitcpio preset %s has no literal PRESETS array", presetPath)
	}
	names, ok := literalShellWords(presetValue[1 : len(presetValue)-1])
	if !ok || len(names) == 0 {
		return false, fmt.Sprintf("mkinitcpio preset %s has ambiguous PRESETS", presetPath)
	}
	usesDropIns := false
	for _, name := range names {
		if value, ok := assignments["ALL_options"]; ok && value != "" && value != `''` && value != `""` {
			return false, fmt.Sprintf("mkinitcpio preset %s has ALL_options that may change HOOKS", presetPath)
		}
		if value, ok := assignments[name+"_options"]; ok && value != "" && value != `''` && value != `""` {
			return false, fmt.Sprintf("mkinitcpio preset %s has %s_options that may change HOOKS", presetPath, name)
		}
		selected := assignments[name+"_config"]
		if selected == "" {
			selected = assignments["ALL_config"]
		}
		if selected != "" {
			selected = strings.Trim(selected, `"'`)
			if !filepath.IsAbs(selected) || filepath.Clean(selected) != filepath.Clean(configPath) {
				return false, fmt.Sprintf("mkinitcpio preset %s uses another or dynamic config for %s", presetPath, name)
			}
		} else {
			usesDropIns = true
		}
	}
	return usesDropIns, ""
}
