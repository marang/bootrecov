package main

import (
	"fmt"
	"log"
	"os"

	"github.com/marang/bootrecov/internal/tui"
)

func main() {
	if len(os.Args) != 4 {
		log.Fatalf("usage: guest_smoke <backup-path> <kernel-image> <initramfs-image>")
	}
	backupPath := os.Args[1]
	kernelImage := os.Args[2]
	initramfsImage := os.Args[3]
	b := tui.BootBackup{
		Path:           backupPath,
		KernelImage:    kernelImage,
		InitramfsImage: initramfsImage,
		HasKernel:      true,
		HasInitramfs:   true,
	}
	if err := tui.AddGrubEntry(b); err != nil {
		log.Fatalf("AddGrubEntry: %v", err)
	}
	entries, err := tui.ListGrubEntries()
	if err != nil {
		log.Fatalf("ListGrubEntries: %v", err)
	}
	if len(entries) == 0 {
		log.Fatal("no grub entries found after AddGrubEntry")
	}
	for _, e := range entries {
		if e.BackupPath == backupPath {
			fmt.Printf("ADDED_ID=%s\n", e.ID)
			return
		}
	}
	log.Fatalf("added entry for %q not found in ListGrubEntries", backupPath)
}
