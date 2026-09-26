package cli

import (
	"path"
	"runtime"
	"strings"
	"unicode/utf8"
)

const windowsDefaultFileNameRuneLimit = 120

var windowsReservedFileNames = map[string]struct{}{
	"CON":     {},
	"PRN":     {},
	"AUX":     {},
	"NUL":     {},
	"CONIN$":  {},
	"CONOUT$": {},
	"COM1":    {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {}, "COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {}, "LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

func localAssetFileName(attached asset) string {
	return localFileNameForOS(assetFileName(attached), runtime.GOOS)
}

func localFileNameForOS(name string, goos string) string {
	if goos != "windows" {
		return name
	}
	return windowsSafeFileName(name)
}

func windowsSafeFileName(name string) string {
	name = strings.TrimSpace(name)
	var builder strings.Builder
	for _, char := range name {
		if char < 32 || strings.ContainsRune(`<>:"/\|?*`, char) {
			builder.WriteByte('_')
			continue
		}
		builder.WriteRune(char)
	}

	safe := strings.TrimRight(builder.String(), " .")
	if safe == "" || safe == "." || safe == ".." {
		safe = "attachment"
	}

	extension := path.Ext(safe)
	stem := strings.TrimSuffix(safe, extension)
	if _, reserved := windowsReservedFileNames[strings.ToUpper(stem)]; reserved {
		safe = "_" + safe
		extension = path.Ext(safe)
		stem = strings.TrimSuffix(safe, extension)
	}

	if utf8.RuneCountInString(safe) <= windowsDefaultFileNameRuneLimit {
		return safe
	}

	extensionRunes := []rune(extension)
	if len(extensionRunes) >= windowsDefaultFileNameRuneLimit/2 {
		return string([]rune(safe)[:windowsDefaultFileNameRuneLimit])
	}
	stemBudget := windowsDefaultFileNameRuneLimit - len(extensionRunes)
	stemRunes := []rune(stem)
	if len(stemRunes) > stemBudget {
		stemRunes = stemRunes[:stemBudget]
	}
	return string(stemRunes) + extension
}
