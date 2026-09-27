---
name: go-tui-expert
description: Create visually stunning Go TUIs grounded in Charm best practices and patterns from popular apps (Bubble Tea, Lip Gloss, Bubbles, Crush). Use when designing, implementing, restyling, or reviewing a terminal UI or dashboard; when the user wants a gorgeous, production-quality TUI; when they mention Charm, Crush, Lip Gloss, flicker, alt screen, or /go-tui-expert. Use when the user runs /go-tui-expert.
---

# Go TUI Expert

Goal: ship a **visually stunning** terminal UI, not a functional box of text. Ground every layout and interaction in Charm production practice and in apps people already love (Crush, Glow, Gum, the Lip Gloss layout demo).

Read [references/stack.md](references/stack.md) for modules, Elm architecture, and v1/v2. Read [references/inspiration.md](references/inspiration.md) before inventing a look. Read [references/stability.md](references/stability.md) before any polling, logging, or resize work.

## First moves

1. Detect the project: `go.mod` imports `charm.land/bubbletea/v2` vs `github.com/charmbracelet/bubbletea`. **Match the repo.** Prefer v2 (`charm.land/.../v2`) only for greenfield or when asked to migrate. Never mix v1 and v2.
2. Name two visual references from [references/inspiration.md](references/inspiration.md) and steal their *structure* (chrome, density, focus, help), not their brand colors.
3. Budget the screen before drawing widgets: header / body / footer must sum to `WindowSizeMsg` height. Body is a `viewport` (or equivalent) that eats leftover rows.
4. Keep I/O off the alt screen: no `log.Printf`, no child-process stderr, no HTTP access logs while the TUI owns the terminal.

## Architecture (Elm)

Every screen is `Model` + `Init` + `Update` + `View`.

- **Model** holds only UI state. Long work is a `tea.Cmd` that returns a `Msg`.
- **Update** is a single switch on `tea.Msg`. Subcomponents get `Update` only for messages they own (keys when focused, ticks when spinning).
- **View** is a pure function of Model. No I/O, no mutation, no “maybe fetch.”
- **Cmds** for time, HTTP, `exec`, file tail. Never block `Update`.
- **Redraws:** skip `View` work when a tick/snapshot is unchanged (compare seq, size, status). Spinners tick only while busy.

v2: `View() tea.View` with `v.AltScreen = true` (and mouse/title on the same struct). v1: `View() string` plus `tea.WithAltScreen()` on `NewProgram`. Details in [references/stack.md](references/stack.md).

## Visual craft

Treat the terminal like a designed product.

**Theme tokens** (define once, reuse everywhere):

- `accent` — focus, selection, primary action
- `muted` — labels, help, rules
- `ok` / `warn` / `bad` — status only
- `title` — one bold wordmark

One accent. Status uses semantic colors, not a rainbow.

**Chrome (Crush / Glow density):**

```
wordmark          status
context line (url, key, active model)
────────────────────────────────────
body (viewport / list / chat)  ← remaining height
────────────────────────────────────
help: keys in muted, one line
```

- Header is 1–3 lines. Footer is one help line. Everything else is body.
- Focused region: accent border or `▸`. Unfocused: muted.
- Tabs: Lip Gloss layout demo — joined tab styles + a gap that fills remaining width (`max(0, width - lipgloss.Width(row))`).
- Lists: `▸` cursor, installed/active as short status words (`installed`, `active`), not badges stacked in boxes.
- Dialogs: `lipgloss.Place(width, height, Center, Center, box)` over the frame (Crush overlays).
- Measure with `lipgloss.Width` / `Height` (ANSI-aware). Never `len()` on styled strings.
- `Width()` on a style includes padding and border. Inner content width = outer − padding − border.

**Copy:** short labels, verbs in help (`space start/stop`, `enter send`). No ASCII art logos unless the user asks.

## Layout math (non-negotiable)

```
innerWidth  = termWidth  - horizontalPadding
chromeRows  = count real lines after styling (borders count)
bodyHeight  = termHeight - chromeRows   // min 3
```

If chrome is wrong, iTerm2 wraps leftover cells and the screen “janks.” Nested rounded panels that each set `.Width(termWidth)` will overflow. Prefer a compact header + one body viewport.

On `WindowSizeMsg`, set child widths/heights, then rebuild viewport content. Fill alt-screen height so empty rows are explicit, not terminal wrap.

## Interaction

- Keys documented in the footer; they must work.
- Typing focus: letter shortcuts (`q`, `r`, `i`) only when the input is empty or unfocused.
- v2 space is `msg.String() == "space"`; v1 is `" "`.
- Mouse is optional. If enabled, keep keyboard complete.
- `tea.Program.Send` from goroutines is fine for logs/events; still compare-and-skip to avoid flicker.

## Quality bar (ship only if all pass)

- Looks intentional at 80×24 and at 160×50; resize does not tear.
- Alt screen: no leaked `log` lines, no cursor flying on tick.
- Help line matches real keybindings.
- Busy work shows a spinner **only then**.
- Status (up/down, active model) is readable in one glance.
- View tests: `WindowSizeMsg` then `View()` contains wordmark, status, help.

## Anti-patterns

- Polling `/health` every few seconds **and** logging those requests to stderr.
- `tea.Tick` that always mutates status strings (`refreshing…`) so the header width jumps.
- Magic `chrome := 16` that ignores border/help/input lines.
- Mixing `0.0.0.0` listen logs with the TUI process stdout.
- New widgets for every feature; compose `viewport`, `textinput`, `list`, `spinner`, `help`, `table`.

## When editing this repo

`internal/tui` is a Charm dashboard (currently Bubble Tea v1). Keep its control-plane job (gateway, logs, models). Raise visual quality with tokens, chrome budget, and Crush-like density — do not turn it into a chat app.

## After implementation

Run `go test ./internal/tui/...` and a PTY/manual pass: start, resize, focused input vs shortcuts, no flicker on idle.
