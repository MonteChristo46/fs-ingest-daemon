package util

import (
	"os"

	"golang.org/x/sys/windows"
)

var supportsEmoji bool

func init() {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Stdout, &mode); err == nil {
		mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		_ = windows.SetConsoleMode(windows.Stdout, mode)
	}

	_, hasWT := os.LookupEnv("WT_SESSION")
	supportsEmoji = hasWT
}

func SupportsEmoji() bool {
	return supportsEmoji
}

func Emoji(emoji, fallback string) string {
	if supportsEmoji {
		return emoji
	}
	return fallback
}
