package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTaskOutputIgnoresLateCommandEventsAfterClosingChannel(t *testing.T) {
	output := make(chan string, 1)
	var delayedSink func(string)
	cmd := taskCmdWithOutput(output, func() tea.Msg {
		delayedSink = commandOutputSink.current()
		return nil
	})
	if msg := cmd(); msg != nil {
		t.Fatalf("unexpected task result: %#v", msg)
	}
	if delayedSink == nil {
		t.Fatal("task did not capture command output sink")
	}
	delayedSink("late progress")
	if _, open := <-output; open {
		t.Fatal("task output channel accepted an event after closing")
	}
}

func TestModelRefreshesAfterCompletedRemovalWithCleanupWarning(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      taskKind
		mode      mode
		run       func(string) taskCmdFactory
		completed string
	}{
		{"delete backup", taskDelete, modeBackups, runDeleteTask, "backup deleted: old"},
		{"deactivate backup", taskDeactivate, modeBackups, func(name string) taskCmdFactory {
			return runToggleBackupTask(taskDeactivate, name)
		}, "deactivated: old"},
		{"remove entry", taskRemoveEntry, modeEntries, runRemoveEntryTask, "entry removed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupModuleCleanupTest(t)
			const version = "6.1.0-old"
			makeVersionedBootableBackup(t, SnapshotDir, "old", version)
			makeVersionedBootableBackup(t, EfiDir, "old", version)
			moduleTree := makeMarkedModules(t, version)
			if err := AddGrubEntry(BootBackup{Name: "old"}); err != nil {
				t.Fatal(err)
			}
			backups, entries, refreshError := refreshTaskState()
			if refreshError != "" || len(backups) != 1 || len(entries) != 1 {
				t.Fatalf("initial state: backups=%#v entries=%#v error=%s", backups, entries, refreshError)
			}
			m := newModelComponents()
			m.Backups, m.Entries, m.mode = backups, entries, tc.mode
			m.busy, m.task = true, tc.kind
			m = m.syncComponents()

			moduleCleanupPackageBusy = func(string) (bool, error) {
				return false, errors.New("package database unavailable")
			}
			target := "old"
			if tc.kind == taskRemoveEntry {
				target = entries[0].ID
			}
			msg := tc.run(target)(make(chan string, 64))().(taskDoneMsg)
			if msg.errStatus != "" {
				t.Fatalf("completed mutation reported as failure: %s", msg.errStatus)
			}
			updated, _ := m.Update(msg)
			m = updated.(Model)
			if !strings.Contains(m.status, tc.completed) || !strings.Contains(m.status, "restored module cleanup incomplete: retain restored modules: check package transaction: package database unavailable") {
				t.Fatalf("status does not show completed mutation and cleanup warning: %q", m.status)
			}
			if !strings.Contains(m.viewString(), "module cleanup incomplete") {
				t.Fatalf("cleanup warning is not visible in the TUI: %s", m.viewString())
			}
			if m.busy || len(m.Entries) != 0 {
				t.Fatalf("entry list was not refreshed: busy=%v entries=%#v", m.busy, m.Entries)
			}
			if tc.kind == taskDelete {
				if len(m.Backups) != 0 {
					t.Fatalf("deleted backup remains in TUI: %#v", m.Backups)
				}
				if _, err := os.Stat(filepath.Join(SnapshotDir, "old")); !os.IsNotExist(err) {
					t.Fatalf("snapshot remains after deletion: %v", err)
				}
			} else if len(m.Backups) != 1 || !m.Backups[0].HasSnapshot || m.Backups[0].GrubEntryExists {
				t.Fatalf("backup state was not refreshed: %#v", m.Backups)
			}
			if _, err := os.Stat(moduleTree); err != nil {
				t.Fatalf("cleanup warning should retain module tree: %v", err)
			}
		})
	}
}

func TestModelPreservesMutationFailureBeforeRemoval(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "keep")
	backups, entries, refreshError := refreshTaskState()
	if refreshError != "" {
		t.Fatal(refreshError)
	}
	m := newModelComponents()
	m.Backups, m.Entries = backups, entries
	m.busy, m.task = true, taskDelete
	m = m.syncComponents()
	msg := runDeleteTask("../escape")(make(chan string, 64))().(taskDoneMsg)
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if !strings.HasPrefix(m.status, "delete failed:") || len(m.Backups) != 1 || m.Backups[0].Name != "keep" {
		t.Fatalf("pre-mutation error was misreported: status=%q backups=%#v", m.status, m.Backups)
	}
	if _, err := os.Stat(filepath.Join(snap, "keep")); err != nil {
		t.Fatalf("unrelated snapshot changed: %v", err)
	}
}

func TestRuntimeDependenciesRequireFile(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	t.Setenv("PATH", t.TempDir())
	err := CheckRuntimeDependencies()
	var deps *RuntimeDependenciesError
	if !errors.As(err, &deps) || !containsStringWithPrefix(deps.Missing, "file") {
		t.Fatalf("expected missing file runtime dependency, got %v", err)
	}
}

func TestReconcileCleanupWarningPreservesCompletedState(t *testing.T) {
	for _, useModel := range []bool{false, true} {
		name := "API"
		if useModel {
			name = "TUI"
		}
		t.Run(name, func(t *testing.T) {
			setupModuleCleanupTest(t)
			const version = "6.1.0-old"
			makeVersionedBootableBackup(t, SnapshotDir, "old", version)
			makeVersionedBootableBackup(t, EfiDir, "old", version)
			moduleTree := makeMarkedModules(t, version)
			if err := AddGrubEntry(BootBackup{Name: "old"}); err != nil {
				t.Fatal(err)
			}
			backups, entries, err := RefreshBackupsAndGrub()
			if err != nil || len(entries) != 1 {
				t.Fatalf("initial state: entries=%#v err=%v", entries, err)
			}
			if err := os.RemoveAll(filepath.Join(SnapshotDir, "old")); err != nil {
				t.Fatal(err)
			}
			cause := errors.New("package database unavailable")
			moduleCleanupPackageBusy = func(string) (bool, error) { return false, cause }
			if useModel {
				m := newModelComponents()
				m.Backups, m.Entries = backups, entries
				m.busy, m.task = true, taskReconcile
				m = m.syncComponents()
				msg := runReconcileTask(make(chan string, 64))().(taskDoneMsg)
				if msg.errStatus != "" {
					t.Fatalf("completed reconcile reported as failure: %s", msg.errStatus)
				}
				updated, _ := m.Update(msg)
				m = updated.(Model)
				if m.busy || !strings.Contains(m.status, "reconcile complete") || !strings.Contains(m.status, cause.Error()) || !strings.Contains(m.viewString(), "module cleanup incomplete") {
					t.Fatalf("completed reconcile warning missing: busy=%v status=%q view=%s", m.busy, m.status, m.viewString())
				}
				backups, entries = m.Backups, m.Entries
			} else {
				backups, entries, err = SyncBackupsAndGrub()
				var warning *ReconcileCleanupWarning
				if !errors.As(err, &warning) || !errors.Is(err, cause) {
					t.Fatalf("expected typed cleanup warning preserving cause, got %v", err)
				}
			}
			if len(entries) != 0 || len(backups) != 1 || backups[0].HasSnapshot || backups[0].HasEFI || backups[0].GrubEntryExists {
				t.Fatalf("reconcile returned stale state: backups=%#v entries=%#v", backups, entries)
			}
			if _, err := os.Stat(moduleTree); err != nil {
				t.Fatalf("failed cleanup should retain modules: %v", err)
			}
		})
	}
}

func TestRuntimeDependenciesDoNotRequireFileOutsideArch(t *testing.T) {
	for _, platform := range []string{PlatformFedora, PlatformUbuntu, PlatformDebian} {
		t.Run(platform, func(t *testing.T) {
			boot, snap, efi, grub := setupDirs(t)
			setTestGlobals(t, boot, snap, efi, grub)
			activePlatformID = platform
			t.Setenv("PATH", t.TempDir())
			if err := CheckRuntimeDependencies(); err != nil {
				t.Fatalf("non-Arch startup should not require cleanup-only file command: %v", err)
			}
		})
	}
}

func TestReconcileRefreshesModulesAfterSuccessfulCleanupAndClearsEntries(t *testing.T) {
	setupModuleCleanupTest(t)
	const version = "6.1.0-old"
	makeVersionedBootableBackup(t, SnapshotDir, "inactive", version)
	writeFileWithContent(t, archivedModuleImagePath(filepath.Join(SnapshotDir, "inactive"), version), "archive")
	moduleTree := makeMarkedModules(t, version)
	backups, err := DiscoverBackups()
	if err != nil || len(backups) != 1 || !backups[0].HasRootModules {
		t.Fatalf("initial module state: backups=%#v err=%v", backups, err)
	}
	m := newModelComponents()
	m.Backups = backups
	// An entry removed externally must disappear even when its file is absent.
	m.Entries = []GrubEntry{{Name: "removed-externally"}}
	m.busy, m.task = true, taskReconcile
	m = m.syncComponents()
	msg := runReconcileTask(make(chan string, 64))().(taskDoneMsg)
	if msg.errStatus != "" {
		t.Fatalf("reconcile failed: %s", msg.errStatus)
	}
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if len(m.Entries) != 0 || len(m.Backups) != 1 || m.Backups[0].HasRootModules || !m.Backups[0].HasArchivedModules {
		t.Fatalf("cleanup state was not refreshed: backups=%#v entries=%#v", m.Backups, m.Entries)
	}
	if _, err := os.Stat(moduleTree); !os.IsNotExist(err) {
		t.Fatalf("unused archived snapshot modules should be cleaned: %v", err)
	}
}
