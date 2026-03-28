# Sprint 002 — First Public Release

Get claudebar installable and visible. Depends on sprint 001 completing.

## Tasks

- [ ] Set up GoReleaser (.goreleaser.yaml + GitHub Actions)
- [x] Create Homebrew tap (lableaks/homebrew-tap) — repo created
- [ ] Configure GoReleaser to push Homebrew formula to tap
- [ ] Write install.sh (curl | sh installer)
- [ ] VHS demo recording (.tape script + GIF for README)
- [ ] Update README with install instructions (brew, curl, go install)
- [ ] First tagged release (v0.1.0)

## Ordering

1. GoReleaser + VHS tape (parallel)
2. install.sh + README update (parallel, after GoReleaser)
3. Tag v0.1.0 (last)

## Out of scope

- Feature work beyond what ships in v0.1.0
