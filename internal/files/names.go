package files

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var reservedStagingNames = map[string]bool{
	"CON":     true,
	"PRN":     true,
	"AUX":     true,
	"NUL":     true,
	"CLOCK$":  true,
	"CONIN$":  true,
	"CONOUT$": true,
	"COM1":    true,
	"COM2":    true,
	"COM3":    true,
	"COM4":    true,
	"COM5":    true,
	"COM6":    true,
	"COM7":    true,
	"COM8":    true,
	"COM9":    true,
	"LPT1":    true,
	"LPT2":    true,
	"LPT3":    true,
	"LPT4":    true,
	"LPT5":    true,
	"LPT6":    true,
	"LPT7":    true,
	"LPT8":    true,
	"LPT9":    true,
}

func ValidName(name string) bool {
	if !utf8.ValidString(name) {
		return false
	}
	length := utf8.RuneCountInString(name)
	if length < 1 || length > 128 {
		return false
	}
	for index, char := range name {
		if index == 0 {
			if !unicode.IsLetter(char) && !unicode.IsNumber(char) {
				return false
			}
			continue
		}
		if unicode.IsLetter(char) || unicode.IsNumber(char) {
			continue
		}
		switch char {
		case '.', '_', '-':
			continue
		default:
			return false
		}
	}
	if strings.HasSuffix(name, ".") {
		return false
	}
	if isPartName(name) {
		return false
	}
	base := name
	if index := strings.Index(base, "."); index >= 0 {
		base = base[:index]
	}
	return !reservedStagingNames[strings.ToUpper(base)]
}

func isPartName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".part")
}
