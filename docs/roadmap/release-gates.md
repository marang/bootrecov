# Release Gates

## Baseline Gates

Every release must pass:

- clean git tree before tagging
- `make test`
- `make test-bootvm`
- `make test-bootvm-grub-matrix` before promoting Ubuntu/Debian GRUB support or changing platform/bootloader detection
- README, SAFETY, AGENTS, and roadmap support status aligned with actual behavior
- no new mutating boot path without explicit risk acknowledgement

## Support Promotion Gates

Before moving a platform or bootloader from detected to experimental:

- detection tests exist
- unsupported and ambiguous cases are tested
- docs state the limitation clearly
- mutating commands are blocked unless the adapter is explicitly implemented

Before moving from experimental to supported:

- unit tests cover entry rendering, parsing, cleanup, and error paths
- VM/E2E test boots through a Bootrecov-managed entry
- release workflow includes the relevant gate or documents why it is manual
- SAFETY documents all boot-critical paths touched by that backend

Before enabling package-manager hooks:

- hook trigger scope is documented
- the hook cannot hang on interactive prompts
- hook commands include explicit risk acknowledgement
- failure behavior is safe during package transactions
