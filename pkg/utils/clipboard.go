package utils

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
)

// CopyToClipboard copies the given text to the system clipboard.
// It first attempts to use the OSC52 terminal escape sequence, which works
// across SSH sessions without requiring external tools (as long as the terminal
// emulator supports OSC52). If OSC52 doesn't work or the terminal doesn't support it,
// it falls back to platform-specific clipboard tools:
// - clip.exe on Windows (including Git Bash)
// - xclip/xsel on Linux
// - pbcopy on macOS
//
// When running inside tmux or GNU screen the sequence is wrapped so that it is
// passed through to the outer terminal.
func CopyToClipboard(text string) error {
	// First, try OSC52 (works with terminal support)
	if err := copyViaOSC52(text); err == nil {
		return nil
	}

	// Fall back to platform-specific tools for SSH sessions without OSC52 support
	return copyViaClipboardTool(text)
}

func copyViaOSC52(text string) error {
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

// copyViaClipboardTool attempts to copy using platform-specific clipboard tools.
// On Windows, tries clip.exe. On Linux, tries xclip then xsel. On macOS, tries pbcopy.
func copyViaClipboardTool(text string) error {
	textBytes := []byte(text)

	// Windows clipboard fallback: works from PowerShell, CMD, and Git Bash.
	if _, err := exec.LookPath("clip.exe"); err == nil {
		cmd := exec.Command("clip.exe")
		cmd.Stdin = bytes.NewReader(textBytes)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Try xclip first (most common on Linux)
	if _, err := exec.LookPath("xclip"); err == nil {
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = bytes.NewReader(textBytes)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Try xsel (Debian/Ubuntu standard)
	if _, err := exec.LookPath("xsel"); err == nil {
		cmd := exec.Command("xsel", "--clipboard", "--input")
		cmd.Stdin = bytes.NewReader(textBytes)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Try pbcopy (macOS)
	if _, err := exec.LookPath("pbcopy"); err == nil {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = bytes.NewReader(textBytes)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// No clipboard tool available
	return fmt.Errorf("clipboard: no clipboard tool available (tried clip.exe, xclip, xsel, pbcopy) and OSC52 is not supported by the terminal")
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
