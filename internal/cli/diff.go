package cli

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	diffAdd = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	diffDel = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
)

// diffLines computes a minimal line diff between old and new text: shared
// prefix/suffix are trimmed, the changed middle is emitted as -old/+new lines
// with up to 2 context lines around it.
func diffLines(oldText, newText string) []string {
	old := strings.Split(oldText, "\n")
	news := strings.Split(newText, "\n")

	prefix := 0
	for prefix < len(old) && prefix < len(news) && old[prefix] == news[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(news)-prefix &&
		old[len(old)-1-suffix] == news[len(news)-1-suffix] {
		suffix++
	}

	oldMid := old[prefix : len(old)-suffix]
	newMid := news[prefix : len(news)-suffix]
	if len(oldMid) == 0 && len(newMid) == 0 {
		return nil
	}

	var lines []string
	ctxBefore := 2
	if prefix >= ctxBefore {
		lines = append(lines, old[prefix-ctxBefore:prefix]...)
	} else if prefix > 0 {
		lines = append(lines, old[:prefix]...)
	}
	for _, l := range oldMid {
		lines = append(lines, "-"+l)
	}
	for _, l := range newMid {
		lines = append(lines, "+"+l)
	}
	if suffix >= 2 {
		lines = append(lines, old[len(old)-suffix:len(old)-suffix+2]...)
	} else if suffix > 0 {
		lines = append(lines, old[len(old)-suffix:]...)
	}
	return lines
}

// renderDiff renders the line diff with green additions and red deletions,
// truncating each line to the given width.
func renderDiff(oldText, newText string, width int) []string {
	var out []string
	for _, l := range diffLines(oldText, newText) {
		switch {
		case strings.HasPrefix(l, "+"):
			out = append(out, diffAdd.Render(trunc(l, width)))
		case strings.HasPrefix(l, "-"):
			out = append(out, diffDel.Render(trunc(l, width)))
		default:
			out = append(out, " "+trunc(l, width))
		}
	}
	return out
}
