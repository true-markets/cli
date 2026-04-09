package cli

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// ANSI color codes using True Markets brand palette.
//
//	Cyan (#00D1FF)  — brand accent, headers, banner
//	Blue (#3B82F6)  — links, info
//	Green (#22C55E) — success, long side
//	Red (#EF4444)   — error, short side
//	Yellow          — warnings, open status
//	White           — primary text
//	Gray            — secondary/dim text
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorCyan   = "\033[38;2;29;78;216m"   // Deep Blue (#1D4ED8)
	colorBlue   = "\033[38;2;59;130;246m"  // #3B82F6
	colorGreen  = "\033[38;2;34;197;94m"   // #22C55E
	colorRed    = "\033[38;2;239;68;68m"   // #EF4444
	colorYellow = "\033[38;2;250;204;21m"  // #FACC15
	colorWhite  = "\033[97m"
	colorGray   = "\033[38;2;156;163;175m" // #9CA3AF
)

// colorsEnabled is set at init time. We disable colors when NO_COLOR env var
// is set.
var colorsEnabled = true

func init() {
	if os.Getenv("NO_COLOR") != "" {
		colorsEnabled = false
		return
	}
	enableWindowsVT()
}

// enableWindowsVT enables ANSI/VT processing on Windows consoles so that the
// escape sequences render correctly.
func enableWindowsVT() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	handle := syscall.Handle(os.Stdout.Fd())

	var mode uint32
	r, _, _ := getConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return // not a console
	}

	const enableVirtualTerminalProcessing = 0x0004
	_, _, _ = setConsoleMode.Call(uintptr(handle), uintptr(mode|enableVirtualTerminalProcessing))
}

// ─── Color helpers ──────────────────────────────────────────────────────────

func cyan(s string) string {
	if !colorsEnabled {
		return s
	}
	return colorCyan + s + colorReset
}

func cyanBold(s string) string {
	if !colorsEnabled {
		return s
	}
	return colorCyan + colorBold + s + colorReset
}

func blue(s string) string {
	if !colorsEnabled {
		return s
	}
	return colorBlue + s + colorReset
}

func green(s string) string {
	if !colorsEnabled {
		return s
	}
	return colorGreen + s + colorReset
}

func red(s string) string {
	if !colorsEnabled {
		return s
	}
	return colorRed + s + colorReset
}

func yellow(s string) string {
	if !colorsEnabled {
		return s
	}
	return colorYellow + s + colorReset
}

func dim(s string) string {
	if !colorsEnabled {
		return s
	}
	return colorDim + s + colorReset
}

func bold(s string) string {
	if !colorsEnabled {
		return s
	}
	return colorBold + s + colorReset
}

// colorizeStatus returns a status string with appropriate color.
func colorizeStatus(status string) string {
	switch status {
	case betStatusOpen:
		return yellow(status)
	case betStatusMatched:
		return cyan(status)
	case betStatusSettled:
		return green(status)
	case betStatusExpired:
		return red(status)
	default:
		return status
	}
}

// colorizeSide returns a side string colored green (long) or red (short).
func colorizeSide(side string) string {
	switch side {
	case betSideLong:
		return green(side)
	case betSideShort:
		return red(side)
	default:
		return side
	}
}

// colorizePrice formats and colors a price string.
func colorizePrice(prefix string, price float64) string {
	return cyan(fmt.Sprintf("%s$%.2f", prefix, price))
}
