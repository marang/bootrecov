# Bootrecov Recovery Platform Roadmap

## Purpose

This document turns the current `bootrecov` utility into a larger product roadmap.
The target is not just "backup `/boot` and add GRUB entries", but a Linux boot recovery platform with:

- safer update preparation
- better snapshot inspection
- more powerful automation
- stronger boot validation
- clearer operational recovery workflows

The intent is to sequence this work in pragmatic phases instead of trying to build everything at once.

## Product Direction

The long-term direction is:

- create and manage reliable `/boot` snapshots
- activate and validate fallback boot paths
- understand why a snapshot exists and whether it is trustworthy
- support operator-driven recovery and eventually safe automated fallback

Current core is:

- TUI backup browser
- Cobra CLI
- snapshot creation
- EFI mirror activation/deactivation
- GRUB custom entry management
- platform detection for Arch and Ubuntu/Debian
- bootloader detection for GRUB and systemd-boot
- generic bootloader CLI entrypoints backed by the GRUB adapter
- pacman pre-transaction hook installation
- rootless VM integration harness

## Guiding Principles

- Linux-only is a feature, not a bug. Do not dilute the design with fake cross-platform support.
- Safety beats convenience for any workflow that changes GRUB, EFI state, or next-boot behavior.
- Inspection and auditability should be first-class.
- New automation must be backed by test coverage, preferably including VM-based validation.
- Distro-specific behavior should be explicit through adapters or configuration, not hidden assumptions.

## Execution Strategy

Recommended delivery order:

1. Expand platform/bootloader adapters without weakening existing GRUB safety.
2. Improve operator visibility and CLI ergonomics.
3. Add metadata, retention, and machine-readable outputs.
4. Add controlled boot orchestration.
5. Add automated validation and failure handling.
6. Add repair workflows and broader distro support.

## Phase 0: Adapter Foundation

Goal:

- decouple platform and bootloader assumptions from snapshot safety logic
- prove a second platform through Ubuntu/Debian detection while keeping GRUB as the only supported bootloader backend

Current status:

- `bootrecov doctor`
- platform detection through `/etc/os-release`
- bootloader detection for GRUB and systemd-boot
- environment overrides for platform, bootloader, boot dir, ESP dir, and EFI mirror dir
- generic `bootrecov bootloader ...` commands
- Arch/pacman hook support retained
- Ubuntu/Debian apt hooks intentionally not implemented yet
- systemd-boot detected but rejected for mutating operations

Next work:

- move GRUB code behind a smaller explicit backend type
- add Ubuntu/Debian VM scenario to the release gate
- design apt/dpkg hook support separately before enabling it
- implement systemd-boot entry management only after backend tests are ready

## Phase 1: Operator-Focused CLI and Inspection

Goal:

- make the tool easier to use without the TUI
- make snapshots inspectable before booting them

Scope:

- `bootrecov doctor`
- `bootrecov backup inspect <name>`
- `bootrecov backup diff <current|a> <b>`
- `bootrecov status --json`
- richer `backup list` output and filtering
- snapshot detail view in the TUI
- command preview pane for GRUB recovery commands

Deliverables:

- dependency, path, and config diagnostics
- file-level and metadata-level snapshot inspection
- JSON output suitable for scripts and external automation
- better operator confidence before activation or deletion

Rough estimate:

- `1-2 weeks`

Dependencies:

- stable metadata model for snapshot fields
- clear output schema for JSON commands

## Phase 2: Snapshot Metadata and Policy

Goal:

- turn snapshots from unnamed copies into managed recovery objects

Scope:

- snapshot manifest file per backup
- package transaction metadata for pacman-triggered snapshots
- file lists and checksums
- activation history
- retention policies
- pinned snapshots
- prune command for unactivated snapshots

Deliverables:

- snapshot manifest design
- retention policy engine
- pin/unpin commands
- snapshot provenance: why it exists, when it was created, and from which transaction

Rough estimate:

- `2-4 weeks`

Dependencies:

- Phase 1 inspection output should help shape manifest contents
- need clear compatibility rules for old snapshots without manifests

## Phase 3: Boot Orchestration and Safer Recovery

Goal:

- move from "entry exists" to controlled boot recovery workflows

Scope:

- set next boot to a chosen recovery entry
- confirmation flow after successful recovery boot
- one-shot boot fallback logic
- explicit "mark current boot as trusted" workflow
- CLI and TUI support for next-boot management

Deliverables:

- `bootrecov boot next <snapshot>`
- `bootrecov boot confirm`
- recovery-state metadata tying boot outcomes to snapshots
- safer remote or unattended reboot workflows

Rough estimate:

- `3-5 weeks`

Dependencies:

- requires careful GRUB/default-entry handling
- should be backed by stronger integration tests before release

## Phase 4: Automated Validation and Failure Handling

Goal:

- know whether activated backups are actually bootable
- eventually make controlled automatic fallback possible

Scope:

- richer VM test assertions
- boot validation records per snapshot
- boot confidence scoring
- detect failed boots or missing confirmation
- prepare automatic fallback policy design

Deliverables:

- per-snapshot validation status
- boot confidence criteria
- repeatable VM checks against activated entries
- design doc for automatic fallback safety model

Rough estimate:

- `4-8 weeks`

Dependencies:

- Phase 3 boot orchestration
- more robust integration harness and test fixtures

## Phase 5: Recovery Workspace and Repair Mode

Goal:

- help users fix broken systems, not just boot older states

Scope:

- mount and chroot helpers
- repair-session guidance
- bind mount orchestration
- snapshot-backed troubleshooting workflow
- rescue bundle export to external media or tarball

Deliverables:

- `bootrecov repair prepare <snapshot>`
- guided repair workflow docs and CLI support
- rescue export format with manifest and checksums

Rough estimate:

- `4-8 weeks`

Dependencies:

- should not begin until metadata and validation models are stable
- needs extremely careful privilege and cleanup handling

## Cross-Cutting Workstreams

These should progress across all phases:

### Distro Adapters

- Arch
- Debian/Ubuntu
- Fedora
- mkinitcpio vs dracut
- varying `grub.cfg` output paths

### Output and UX

- stable JSON output
- consistent table output
- TUI detail panes and clearer status messages
- explicit warnings for risky actions

### Testing

- unit coverage for new policy and metadata logic
- rootless VM integration tests for critical workflows
- release gating for boot-critical changes

### Documentation

- keep `README.md` current
- keep `AGENTS.md` current
- add operator-facing playbooks for common failure scenarios

## Suggested Milestones

### Milestone A: CLI + Doctor + Inspect

Includes:

- `doctor`
- `inspect`
- `diff`
- `status --json`
- improved TUI detail display

Target value:

- immediate operator usability gain

Estimated time:

- `2-4 weeks`

### Milestone B: Metadata + Retention

Includes:

- manifests
- package metadata
- pinning
- prune policies

Target value:

- snapshots become manageable, not just enumerable

Estimated time:

- `3-5 weeks`

### Milestone C: Controlled Recovery Boot

Includes:

- next-boot selection
- confirmation
- boot trust markers

Target value:

- meaningful operational recovery workflow

Estimated time:

- `3-5 weeks`

### Milestone D: Validation and Automation

Includes:

- VM-based boot validation
- confidence scoring
- failed-boot handling design

Target value:

- strong confidence in recovery safety

Estimated time:

- `4-8 weeks`

## Rough Overall Timeline

For one focused engineer:

- first meaningful expansion milestone: `2-4 weeks`
- medium maturity: `6-10 weeks`
- serious recovery platform: `3-6 months`

The main constraint is not coding speed. It is the safety bar for anything that mutates boot state or automates fallback behavior.

## What Not To Do Yet

Avoid these too early:

- pretending to support non-Linux systems
- adding bootloader abstraction before GRUB flows are mature
- adding automatic fallback before confirmation and validation exist
- shipping repair mode before mount/cleanup behavior is heavily tested
- over-designing manifests before CLI inspection surfaces are proven useful

## Immediate Recommendation

Start with Milestone A.

Best next features:

- `bootrecov doctor`
- `bootrecov backup inspect`
- `bootrecov backup diff`
- `bootrecov status --json`
- improved TUI detail view

That block is small enough to deliver soon and large enough to materially improve the tool.
