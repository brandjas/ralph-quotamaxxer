// Package notermquery stops Bubble Tea v1 from querying the terminal at
// program startup. Import it for side effects only.
//
// bubbletea v1's package init calls lipgloss.HasDarkBackground(), which
// writes OSC 11 / DSR queries to the terminal and blocks up to 5s reading
// the reply from stdin. Because init runs for every quotamaxxer invocation
// (not just `monitor`), that meant:
//
//   - a 5s startup delay on terminals that don't answer (some multiplexers,
//     CI ptys, devcontainer shells),
//   - user keystrokes typed during that window being swallowed instead of
//     reaching claude,
//   - stray escape sequences emitted around the statusline and guard output.
//
// Presetting the background in the default lipgloss renderer makes
// HasDarkBackground return without querying. quotamaxxer only uses fixed ANSI
// colors, so the value is never consulted. If lipgloss.AdaptiveColor is ever
// introduced, revisit this.
//
// Ordering: since Go 1.21, packages are initialized in lexical import-path
// order once their dependencies are ready. This package and bubbletea both
// depend on lipgloss, and "github.com/brandjas/..." sorts before
// "github.com/charmbracelet/bubbletea", so this init always runs first.
package notermquery

import "github.com/charmbracelet/lipgloss"

func init() {
	lipgloss.SetHasDarkBackground(true)
}
