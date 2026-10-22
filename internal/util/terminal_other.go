//go:build !windows

package util

var supportsEmoji = true

func SupportsEmoji() bool {
	return supportsEmoji
}

func Emoji(emoji, fallback string) string {
	return emoji
}
