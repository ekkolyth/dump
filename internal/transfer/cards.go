package transfer

import (
	"os"
	"regexp"
	"strconv"
)

var cardFolderRe = regexp.MustCompile(`^CARD (\d+)$`)

// 0 if none
func HighestCardNumber(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	highest := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		match := cardFolderRe.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		number, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if number > highest {
			highest = number
		}
	}
	return highest
}
