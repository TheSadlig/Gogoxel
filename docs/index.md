# Gogoxel Documentation

This directory is the source of the public documentation site (issue #9).
It is wired in incrementally — the first slice ships this index plus the
public-API outline. A static-site generator (mkdocs / Hugo) will be
selected in a follow-up PR.

## Layout

- `index.md` — this file
- `getting-started.md` — quickstart guide
- `architecture.md` — high-level system map
- `api/` — reference (auto-generated from godoc in a future slice)

## Authoring rules

- Every public API change must update the relevant doc page in the
  same commit.
- Code samples in docs must compile (a `docs-check` make target will
  enforce this in a future slice).
