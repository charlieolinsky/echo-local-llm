# Stability (flicker, iTerm2, logs)

Stunning dies if the frame jitters.

## Alt screen vs stdout

The program that runs `tea.NewProgram` owns the TTY. Anything else writing to stdout/stderr (HTTP middleware, `log.Default()`, child `serve` without `io.Discard`) paints *into* the alt screen. iTerm2 then wraps those cells; the UI looks possessed.

Rules:

- TUI process: `log.SetOutput(io.Discard)` for the session (restore after `Run`).
- HTTP access logs go to a file or ring buffer the TUI *reads*, never to the TTY.
- Child processes: `cmd.Stdout = io.Discard`, `cmd.Stderr = io.Discard` (or the same log file). `Setpgid: true` so quitting the TUI does not kill a detached server.
- Skip logging `/health` even in headless mode if anything polls it.

## Do not poll the painted surface

Idle ticks that hit `/health` and always assign `status = "refreshing…"` change header width and force a full View. Probe with TCP or a silent file-size check. Apply a `Msg` only when gateway/ollama/log-seq **changed**.

Spinners: `Init` must not start a spinner. Start `spinner.Tick` when busy; stop forwarding `TickMsg` when idle.

## Height budget

Lip Gloss `Width` fills with spaces so background colors extend. iTerm2 wraps those spaces on resize ([bubbletea#544](https://github.com/charmbracelet/bubbletea/discussions/544)). Mitigations:

- Alt screen + exact `bodyHeight = termHeight - chromeLines`
- Pad the frame to `termHeight` with empty lines if needed
- Do not set every nested box to the full terminal width
- Count border lines (a `NormalBorder` input is 3 rows, not 1)

`WindowSizeMsg` is the only source of truth for size. Cache inner width; recompute chrome after style changes.

## Follow vs jump

Log viewports: `GotoBottom()` only if follow is on or the user was already at the bottom (`AtBottom()`). New lines must not yank a user who scrolled up.

## Testing

- Unit: inject `WindowSizeMsg{Width: 80, Height: 24}` and `160x50`; assert View contains chrome and does not panic.
- `tea.WithWindowSize` / `WithColorProfile` (v2) for golden-ish tests.
- Manual/PTY: idle 5s with no visual change; resize; type in the prompt including letters that are also shortcuts.
