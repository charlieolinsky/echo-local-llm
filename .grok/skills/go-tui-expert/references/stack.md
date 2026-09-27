# Charm stack

## Modules

| Job | v2 (greenfield) | v1 (legacy) |
|-----|-----------------|-------------|
| Runtime | `charm.land/bubbletea/v2` | `github.com/charmbracelet/bubbletea` |
| Style/layout | `charm.land/lipgloss/v2` | `github.com/charmbracelet/lipgloss` |
| Widgets | `charm.land/bubbles/v2` | `github.com/charmbracelet/bubbles` |
| Markdown | `charm.land/glamour/v2` | `github.com/charmbracelet/glamour` |

Upgrade all three together. Crush production: Bubble Tea v2 + Lip Gloss v2 + Bubbles v2 + Glamour v2; screen buffer via `github.com/charmbracelet/ultraviolet`.

v2 rendering: Cursed Renderer (ncurses-style cell diffs), synchronized updates (mode 2026), color downsampling inside Bubble Tea. Lip Gloss v2 is I/O-pure; Bubble Tea owns the TTY so the two no longer deadlock on queries.

## Elm loop

```
Init() Cmd  →  Msg  →  Update(msg) (Model, Cmd)  →  View(Model)
                              ↑                         |
                              └──────── msgs ───────────┘
```

Cmds are functions `func() Msg` (HTTP, sleep, exec). `tea.Batch` / `tea.Sequence` (v2; v1 had `Sequentially`).

Bubbles v2: getters/setters (`SetWidth`, `SetHeight`) instead of exported `Width`/`Height` fields. Light/dark: `tea.RequestBackgroundColor` in `Init`, then style — Lip Gloss v2 dropped `AdaptiveColor`.

## v2 View (declarative)

```go
func (m model) View() tea.View {
    v := tea.NewView(content)
    v.AltScreen = true
    v.MouseMode = tea.MouseModeCellMotion
    v.WindowTitle = "local-llm"
    return v
}
```

| v1 | v2 |
|----|----|
| `View() string` | `View() tea.View` |
| `tea.WithAltScreen()` | `v.AltScreen = true` |
| `tea.KeyMsg` + `case " ":` | `tea.KeyPressMsg` + `case "space":` |
| `tea.WithMouseCellMotion()` | `v.MouseMode = tea.MouseModeCellMotion` |

Full upgrade: https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md

## Layout primitives (Lip Gloss)

- `JoinHorizontal(pos, ...string)` / `JoinVertical(pos, ...string)` — `Top`/`Center`/`Bottom` or `Left`/`Right`
- `Place(w, h, hAlign, vAlign, s)` — dialogs and empty-frame fill
- `Width`/`Height`/`MaxWidth`/`MaxHeight` on styles
- `lipgloss.Width(s)` / `lipgloss.Height(s)` for ANSI-aware measure

Border + padding consume cells: inner = `Width - 2*border - padLeft - padRight`.

## Composition (Crush)

Crush: cobra CLI → `internal/app` wiring → `internal/ui` Bubble Tea root. Root `UI` model owns chat, editor, dialogs; routes `tea.Msg`; pubsub for permissions/agent events. Submodels do not print. Overlays sit on the same frame via Place/compositing, not a second program.

Pattern: smart root, dumb widgets. Root owns focus enum; only the focused child receives key `Update`.
