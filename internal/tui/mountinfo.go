package tui

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type mountInfoEntry struct {
	mountPoint string
	fsType     string
}

func findMountPoint(path string) (string, error) {
	entries, err := readMountInfo()
	if err != nil {
		return "", err
	}

	path = filepath.Clean(path)
	best := ""
	for _, entry := range entries {
		mountPoint := filepath.Clean(entry.mountPoint)
		if path != mountPoint && !strings.HasPrefix(path, mountPoint+string(os.PathSeparator)) {
			continue
		}
		if len(mountPoint) > len(best) {
			best = mountPoint
		}
	}
	if best == "" {
		return "", fmt.Errorf("no mount point found for %s", path)
	}
	return best, nil
}

func readMountInfo() ([]mountInfoEntry, error) {
	data, err := os.ReadFile(mountInfoPath)
	if err != nil {
		return nil, err
	}

	var entries []mountInfoEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		entry, ok := parseMountInfoLine(scanner.Text())
		if ok {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func parseMountInfoLine(line string) (mountInfoEntry, bool) {
	parts := strings.Split(line, " - ")
	if len(parts) != 2 {
		return mountInfoEntry{}, false
	}

	fields := strings.Fields(parts[0])
	if len(fields) < 5 {
		return mountInfoEntry{}, false
	}
	fsFields := strings.Fields(parts[1])
	if len(fsFields) < 1 {
		return mountInfoEntry{}, false
	}

	return mountInfoEntry{
		mountPoint: decodeMountInfoPath(fields[4]),
		fsType:     fsFields[0],
	}, true
}

func decodeMountInfoPath(s string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	out := replacer.Replace(s)
	if !strings.Contains(out, `\`) {
		return out
	}

	var b strings.Builder
	for i := 0; i < len(out); i++ {
		if out[i] == '\\' && i+3 < len(out) {
			if v, err := strconv.ParseInt(out[i+1:i+4], 8, 32); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(out[i])
	}
	return b.String()
}
