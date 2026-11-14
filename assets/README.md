# Cobalt Mark assets

`markstack-icon.png` is the selected Cobalt Mark production master: 1024×1024, full bleed, opaque. Both `viva.toml` and the embedded running-window icon use it. Platform shaping is owned by Viva. The approved source was the independent icon master from the 2026-11-11 design session; the concept board is not a runtime screenshot.

`markstack-mark.svg` is the small native titlebar rendition of the same white ribbon and cyan fold.

The functional SVGs in `lucide/` are unmodified Lucide static **0.468.0**, downloaded from `https://cdn.jsdelivr.net/npm/lucide-static@0.468.0/icons/`. Their upstream license is in `lucide/LICENSE`.

Regenerate compiled icons with the pinned Viva tool:

```sh
go tool viva icons -o internal/icons/icons.go -pkg icons -license assets/lucide/LICENSE assets/lucide assets/markstack-mark.svg
```

Export a PNG scale proof on macOS (the master is never overwritten):

```sh
swift scripts/generate_icon.swift assets/markstack-icon.png build/icon-64.png 64
```

The compiled Go resources need no runtime SVG parser or downloaded font. Keep all icons in this version/family; preserve their license when redistributing.
