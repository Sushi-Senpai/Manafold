package synergy

import (
	"bufio"
	_ "embed"
	"io"
	"os"
	"strings"
)

// defaultCommanderList is the embedded default commander list: a modest set of
// popular commanders spread across colour identities.
//
//go:embed commanders.txt
var defaultCommanderList string

// LoadCommanders reads the commander list from path, or the embedded default
// when path is empty (SYN-001).
//
// @spec SYN-001
func LoadCommanders(path string) ([]string, error) {
	if path == "" {
		return ParseCommanderList(strings.NewReader(defaultCommanderList))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseCommanderList(f)
}

// ParseCommanderList reads one commander name per line, ignoring blank lines
// and lines starting with '#'. Names keep their commas ("Krenko, Mob Boss"),
// which is why the list is line-delimited rather than comma-separated.
func ParseCommanderList(r io.Reader) ([]string, error) {
	var names []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		names = append(names, line)
	}
	return names, sc.Err()
}
