package utils

import (
	"encoding/base64"
	"fmt"
	"os"
)

// CopyToClipboard copies the given text to the system clipboard using the
// OSC52 terminal escape sequence. This works across SSH sessions and does not
// require any external clipboard binary (such as xclip or pbcopy), as long as
// the terminal emulator supports OSC52.
//
// When running inside tmux or GNU screen the sequence is wrapped so that it is
// passed through to the outer terminal.
func CopyToClipboard(text string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	sequence := buildOSC52(encoded)

	f, shouldClose, err := openTerminal()
	if err != nil {
		return err
	}
	if shouldClose {
		defer f.Close()
	}

	if _, err := f.WriteString(sequence); err != nil {
		return err
	}

	return nil
}

// buildOSC52 builds the OSC52 escape sequence for the given base64 payload,
// wrapping it for tmux or screen when those multiplexers are detected.
func buildOSC52(encoded string) string {
	base := fmt.Sprintf("\x1b]52;c;%s\x07", encoded)

	term := os.Getenv("TERM")
	switch {
	case os.Getenv("TMUX") != "":
		// tmux requires the escape sequence to be wrapped in its passthrough
		// so it reaches the outer terminal.
		return fmt.Sprintf("\x1bPtmux;\x1b%s\x1b\\", base)
	case len(term) >= 6 && term[:6] == "screen":
		// screen splits the sequence using DCS; keep it simple by wrapping.
		return fmt.Sprintf("\x1bP%s\x1b\\", base)
	default:
		return base
	}
}

// openTerminal returns a writer connected to the controlling terminal so that
// the escape sequence is delivered even when stdout is redirected. The boolean
// return value indicates whether the caller is responsible for closing it.
func openTerminal() (*os.File, bool, error) {
	if f, err := os.OpenFile(terminalDevice, os.O_WRONLY, 0); err == nil {
		return f, true, nil
	}

	// Fall back to stdout when the terminal device is unavailable (e.g. when
	// there is no controlling tty). Never close stdout.
	return os.Stdout, false, nil
}
