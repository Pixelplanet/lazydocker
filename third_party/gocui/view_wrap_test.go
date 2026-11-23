package gocui

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// The lazydocker patch makes draw wrap only the lines that changed since the
// last draw. This checks that doing so always gives the same result as
// wrapping the whole buffer, by applying random sequences of operations to two
// views and forcing one of them to start from scratch on every draw.
func TestIncrementalWrapMatchesFullWrap(t *testing.T) {
	previousScreen := Screen
	defer func() { Screen = previousScreen }()

	Screen = tcell.NewSimulationScreen("")
	if err := Screen.Init(); err != nil {
		t.Fatal(err)
	}
	Screen.SetSize(200, 60)

	pieces := []string{
		"hello world ", "a-b-c-d ", "\n", "\n", "\r", "\t", "日本語のテキスト ",
		"\x1b[31mred\x1b[0m ", "\x1b[K", "loooooooooooooooooooooooooooooooooooooooooooong",
		" ", "x", "line with several words in it that should wrap\n",
	}

	for seed := int64(0); seed < 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		incremental := newView("incremental", 0, 0, 30, 10, OutputNormal)
		full := newView("full", 0, 0, 30, 10, OutputNormal)
		incremental.Wrap, full.Wrap = true, true
		both := func(f func(v *View)) {
			f(incremental)
			f(full)
		}

		for step := 0; step < 400; step++ {
			switch op := rng.Intn(100); {
			case op < 60:
				s := ""
				for n := rng.Intn(6) + 1; n > 0; n-- {
					s += pieces[rng.Intn(len(pieces))]
				}
				both(func(v *View) { _, _ = v.Write([]byte(s)) })
			case op < 63:
				both(func(v *View) { v.Clear() })
			case op < 66:
				both(func(v *View) { v.Reset() })
			case op < 70:
				x, y := rng.Intn(20), rng.Intn(15)
				both(func(v *View) { _ = v.SetWritePos(x, y) })
			case op < 74:
				y := rng.Intn(12)
				both(func(v *View) { v.OverwriteLines(y, "over\nwritten lines here\n") })
			case op < 78:
				y, on := rng.Intn(12), rng.Intn(2) == 0
				both(func(v *View) { _ = v.SetHighlight(y, on) })
			case op < 81:
				both(func(v *View) { v.Wrap = !v.Wrap })
			case op < 85:
				width := 10 + rng.Intn(60)
				both(func(v *View) { v.x1 = width })
			case op < 87:
				both(func(v *View) { v.FlushStaleCells() })
			case op < 89:
				both(func(v *View) { v.SetContent("fresh content\nsecond line") })
			default:
				full.wrappedLines, full.wrappedViewLines = 0, 0
				if err := incremental.draw(); err != nil {
					t.Fatal(err)
				}
				if err := full.draw(); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(incremental.viewLines, full.viewLines) {
					t.Fatalf("seed %d step %d: view lines differ\nincremental: %q\nfull:        %q",
						seed, step, incremental.ViewBufferLines(), full.ViewBufferLines())
				}
			}
		}
	}
}
