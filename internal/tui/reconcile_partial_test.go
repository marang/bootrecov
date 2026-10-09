package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconcileReportsMirrorSyncFailureAlongsideSuccessfulRemoval(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	const version = "6.10.1-disk"
	makeVersionedBootableBackup(t, snap, "active", version)
	makeVersionedBootableBackup(t, efi, "active", version)
	writeFile(t, filepath.Join(RootModulesDir, version, "modules.dep"))
	if err := AddGrubEntry(BootBackup{Name: "active"}); err != nil {
		t.Fatal(err)
	}
	makeBootableBackup(t, snap, "obsolete")
	makeBootableBackup(t, efi, "obsolete")
	rclone := filepath.Join(t.TempDir(), "rclone-fails")
	writeExecutable(t, rclone, "#!/bin/sh\necho 'injected sync failure' >&2\nexit 17\n")
	RcloneBin, RequireRclone = rclone, true

	backups, entries, err := SyncBackupsAndGrub()
	var partial *ReconcilePartialError
	if !errors.As(err, &partial) || len(partial.Issues) != 1 {
		t.Fatalf("sync failure was hidden: backups=%#v entries=%#v err=%v", backups, entries, err)
	}
	issue := partial.Issues[0]
	if issue.Snapshot != "active" || issue.Operation != "sync mirror" || !issue.EntryRetained || !issue.MirrorPresent || issue.BootReady {
		t.Fatalf("wrong failed recovery state: %#v", issue)
	}
	if !strings.Contains(issue.Cause.Error(), "injected sync failure") {
		t.Fatalf("sync cause lost: %v", issue.Cause)
	}
	if len(entries) != 1 || len(backups) != 2 {
		t.Fatalf("partial result omitted current state: backups=%#v entries=%#v", backups, entries)
	}
	if dirExists(filepath.Join(efi, "obsolete")) {
		t.Fatal("independent stale mirror was not removed")
	}
	if len(partial.Completed) == 0 || partial.Completed[0] != (ReconcileAction{Snapshot: "obsolete", Operation: "remove inactive mirror"}) {
		t.Fatalf("successful removal was not reported separately: %#v", partial.Completed)
	}
}

func TestReconcileReportsModuleRestoreFailureAndLostEntry(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	const version = "6.10.1-disk"
	makeVersionedBootableBackup(t, snap, "active", version)
	writeFile(t, filepath.Join(RootModulesDir, version, "modules.dep"))
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "active"), version))
	if err := ActivateBackup("active"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(RootModulesDir, version)); err != nil {
		t.Fatal(err)
	}
	want := errors.New("injected module restore failure")
	restoreModuleTreeFunc = func(string, string) error { return want }
	backups, entries, err := SyncBackupsAndGrub()
	var partial *ReconcilePartialError
	if !errors.As(err, &partial) || !errors.Is(err, want) || len(partial.Issues) != 1 {
		t.Fatalf("restore failure was hidden: backups=%#v entries=%#v err=%v", backups, entries, err)
	}
	issue := partial.Issues[0]
	if issue.Snapshot != "active" || issue.Operation != "restore modules" || issue.EntryRetained || issue.BootReady || len(entries) != 0 {
		t.Fatalf("lost recovery entry was not reported: issue=%#v entries=%#v", issue, entries)
	}
}

func TestReconcileReportsMirrorRemovalFailure(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "inactive")
	makeBootableBackup(t, efi, "inactive")
	want := errors.New("injected removal failure")
	previous := reconcileRemoveMirror
	reconcileRemoveMirror = func(string) error { return want }
	t.Cleanup(func() { reconcileRemoveMirror = previous })
	backups, entries, err := SyncBackupsAndGrub()
	var partial *ReconcilePartialError
	if !errors.As(err, &partial) || !errors.Is(err, want) || len(partial.Issues) != 1 {
		t.Fatalf("remove failure was hidden: backups=%#v entries=%#v err=%v", backups, entries, err)
	}
	issue := partial.Issues[0]
	if issue.Snapshot != "inactive" || issue.Operation != "remove inactive mirror" || issue.EntryRetained || !issue.MirrorPresent {
		t.Fatalf("wrong removal state: %#v", issue)
	}
}

func TestTUIReconcileShowsPartialFailureAndRefreshesState(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	const version = "6.10.1-disk"
	makeVersionedBootableBackup(t, snap, "active", version)
	writeFile(t, filepath.Join(RootModulesDir, version, "modules.dep"))
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "active"), version))
	if err := ActivateBackup("active"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(RootModulesDir, version)); err != nil {
		t.Fatal(err)
	}
	makeBootableBackup(t, snap, "obsolete")
	makeBootableBackup(t, efi, "obsolete")
	restoreModuleTreeFunc = func(string, string) error { return errors.New("injected restore failure") }
	m := newModelComponents()
	m.busy, m.task = true, taskReconcile
	m = m.syncComponents()
	msg := runReconcileTask(make(chan string, 64))().(taskDoneMsg)
	if msg.errStatus != "" || len(msg.backups) != 2 || msg.entries == nil || len(msg.entries) != 0 {
		t.Fatalf("TUI discarded current partial state: %#v", msg)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	for _, phrase := range []string{"reconcile incomplete", "completed: obsolete: remove inactive mirror", "active", "restore modules", "injected restore failure", "entry removed"} {
		if !strings.Contains(m.status, phrase) {
			t.Fatalf("TUI status omitted %q: %q", phrase, m.status)
		}
	}
	if len(m.Backups) != 2 || len(m.Entries) != 0 || m.busy {
		t.Fatalf("TUI did not refresh partial state: backups=%#v entries=%#v busy=%v", m.Backups, m.Entries, m.busy)
	}
}

func TestReconcileKeepsCompletedWorkWhenStaleEntryRemovalFails(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "obsolete")
	makeBootableBackup(t, efi, "obsolete")
	stalePath := filepath.Join(efi, "missing")
	staleID := backupID(stalePath)
	entry := fmt.Sprintf("#!/bin/bash\ncat <<'EOF'\nmenuentry 'Bootrecov %s' --id %s {\n}\nEOF\n", stalePath, staleID)
	if err := os.WriteFile(grub, []byte(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	AutoUpdateGrub = true
	GrubMkconfig = ""
	backups, entries, err := SyncBackupsAndGrub()
	var partial *ReconcilePartialError
	if !errors.As(err, &partial) || len(partial.Completed) != 1 || partial.Completed[0] != (ReconcileAction{Snapshot: "obsolete", Operation: "remove inactive mirror"}) {
		t.Fatalf("completed removal was lost after later GRUB error: backups=%#v entries=%#v err=%v", backups, entries, err)
	}
	if partial.FinalizationError == nil || len(backups) != 1 || dirExists(filepath.Join(efi, "obsolete")) {
		t.Fatalf("partial result lacks finalization failure or current mirror state: backups=%#v err=%v", backups, partial)
	}
}

func TestReconcileKeepsCompletedWorkWhenFinalEntryListFails(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "obsolete")
	makeBootableBackup(t, efi, "obsolete")
	want := errors.New("injected final entry listing failure")
	previous := reconcileListEntries
	calls := 0
	reconcileListEntries = func() ([]GrubEntry, error) {
		calls++
		if calls == 2 {
			return nil, want
		}
		return ListGrubEntries()
	}
	t.Cleanup(func() { reconcileListEntries = previous })
	backups, entries, err := SyncBackupsAndGrub()
	var partial *ReconcilePartialError
	if !errors.As(err, &partial) || !errors.Is(err, want) || len(partial.Completed) != 1 {
		t.Fatalf("completed removal was lost after final listing error: backups=%#v entries=%#v err=%v", backups, entries, err)
	}
	if len(backups) != 1 || dirExists(filepath.Join(efi, "obsolete")) || partial.StateVerified {
		t.Fatalf("partial result claims unverified entry state: backups=%#v err=%v", backups, partial)
	}
}
