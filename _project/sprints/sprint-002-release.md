# Sprint 002 — First Public Release

Get claudebar installable and visible.

## Tasks

- [x] Set up GoReleaser (.goreleaser.yaml + GitHub Actions)
- [x] Create Homebrew tap (lableaks/homebrew-tap)
- [x] Configure GoReleaser to push Homebrew formula to tap
- [x] Write install.sh (curl | sh installer)
- [x] Update README — problem-first, screenshots, install instructions
- [x] First tagged release (v0.1.0)
- [x] Test install methods (install.sh on clean Ubuntu, Homebrew formula, source build)

## Notes

- VHS/asciinema can't record tmux-based apps (alternate screen buffer issue). Screenshots done manually.
- Branch is `master` not `main` — install URLs must use `master`.
- Homebrew tap token: fine-grained PAT with Contents R/W on `lableaks/homebrew-tap`, stored as repo secret `HOMEBREW_TAP_TOKEN`.
