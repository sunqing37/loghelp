# Embedded vt10x

Based on `github.com/hinshun/vt10x` commit `5011da428d02`
(`v0.0.0-20220301184237-5011da428d02`), under the accompanying MIT LICENSE.

LogHelp needs scrollback at the parser's scroll boundary so fast output, ANSI
cursor movement and soft wraps are preserved without polling screen snapshots.
Upstream has no history or scroll callback. This local copy adds a bounded ring
(10,000 lines, at most 1,048,576 cells), viewport access, anchored scrolling,
resize history and erase-saved-lines handling. Alternate-screen and partial
scroll regions do not enter history. History keeps original row widths and is
clipped/padded on resize, without reflow.

The identical platform-independent terminal implementation is shared across
Windows, macOS and Linux; unused PTY ioctl helpers are omitted (ADB owns resize).
Other parser behavior is retained. No upstream tests were copied.

Selection uses row/column boundaries across history and the live screen,
adjusts positions when the history ring evicts rows, and clears on resize,
screen changes, or eviction of selected text. Copy joins soft-wrapped rows.
Selection methods require the same state lock as viewport reads.
