package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

// TerminalAsker prompts the user for approval on stdin/stdout. It satisfies
// agent.Asker so that Ask decisions (permission gates, yolo guardian
// escalations) pause for real user input instead of failing closed.
type TerminalAsker struct{}

func (TerminalAsker) Ask(ctx context.Context, question string, options []string) (string, error) {
	fmt.Fprintf(os.Stderr, "\n⚠️  %s\n", question)
	fmt.Fprintf(os.Stderr, "Options: %s\n> ", strings.Join(options, ", "))
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no input")
	}
	return strings.TrimSpace(sc.Text()), nil
}
