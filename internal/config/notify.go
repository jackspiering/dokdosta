package config

import (
	"regexp"
	"strings"
)

// schemeStart finds the start of the next URL in a list: a comma, then scheme://.
var schemeStart = regexp.MustCompile(`,\s*[A-Za-z][A-Za-z0-9+.\-]*://`)

// SplitURLs splits WHALENURSE_NOTIFY_URLS into the Shoutrrr URLs it holds. They are separated by whitespace or a newline,
// or by a comma that is followed by scheme://. A comma anywhere else belongs to the URL: ntfy's tags=warning,skull and
// telegram's chats=a,b would otherwise be cut in two, and the second half would be an invalid URL that fails to send.
func SplitURLs(v string) []string {
	var out []string
	for _, field := range strings.Fields(v) {
		for field != "" {
			loc := schemeStart.FindStringIndex(field)
			if loc == nil {
				break
			}
			if head := strings.Trim(field[:loc[0]], ","); head != "" {
				out = append(out, head)
			}
			// Keep the scheme; drop the comma.
			field = field[loc[0]+1:]
			field = strings.TrimLeft(field, " \t")
		}
		field = strings.Trim(field, ",")
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}
