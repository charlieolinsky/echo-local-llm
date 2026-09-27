# Steal structure from popular TUIs

Use these as the visual bar. Copy hierarchy and density, not logos.

## Crush (charmbracelet/crush)

Production Charm v2 app. Full-screen alt buffer, wordmark + session/model in a tight header, chat list as the body, editor docked at the bottom, command palette / model picker as centered dialogs. Help is a single muted line (or `?` overlay). Streaming tokens append to a list that auto-scrolls when pinned to bottom.

Steal: overlay dialogs, focus rings, one accent, body list + docked input, skip redraws unless the stream actually grew.

## Glow (charmbracelet/glow)

Markdown pager. Discovery list vs reader. High-contrast markdown via Glamour. Keys from `less` plus `?` for help. Quiet chrome so the document is the hero.

Steal: pager viewport, wrap to inner width, help on `?`, dark/light from terminal background.

## Gum (charmbracelet/gum)

Composable glamorous *pieces*: `input`, `choose`, `spin`, `style`, `join`. Each widget is one job, heavily styled (double border, accent 212, aligned labels).

Steal: one-task screens, `JoinHorizontal` for side-by-side boxes, placeholder + prompt styling, confirm as two pills (OK / Cancel).

## Lip Gloss layout example

`github.com/charmbracelet/lipgloss/examples/layout` (also under `charmbracelet/x/examples/layout`):

- Tab row: inactive tabs + one active tab + a `tabGap` of spaces to the window edge
- Color-stepped title, then a description column
- Dialog: question + two buttons, `Place` centered
- Two lists in `JoinHorizontal`

Steal: tabs with a filling gap, dialog as Place, lists as joined columns — this is the canonical “pretty terminal page.”

## k9s / btop / lazygit (non-Charm, still the bar)

Ops dashboards: status in the header, a main table/log, a detail pane, keys along the bottom. Color is semantic (crash red, ready green). No ornamental boxes around every field.

Steal: scanability. A gateway dashboard should read like k9s (status + table/log), not like a settings form.

## Mapping to this repo

`internal/tui` is a control dashboard (process, logs, models). Closest references: **Crush header density** + **k9s status/log** + **Gum join** for the model catalog. Chat is a secondary dock like Crush’s editor, not the product.
