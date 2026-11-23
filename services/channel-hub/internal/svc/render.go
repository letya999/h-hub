package svc

import (
	"html"
	"strings"

	cv1 "github.com/letya999/h-hub/contracts/channel/v1"
	"github.com/letya999/h-hub/shared/locale"
)

type block struct{ kind, text string }

// parseMarkdown lowers the subset the mock adapter can display into plain blocks.
func parseMarkdown(input string) []block {
	var out []block
	code := false
	for _, line := range strings.Split(input, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			code = !code
			continue
		}
		kind := "paragraph"
		if code {
			kind = "code"
		} else if strings.HasPrefix(trim, "#") {
			kind = "heading"
			trim = strings.TrimSpace(strings.TrimLeft(trim, "#"))
		} else if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") {
			kind = "list"
			trim = strings.TrimSpace(trim[2:])
		} else if strings.HasPrefix(trim, "> ") {
			kind = "quote"
			trim = strings.TrimSpace(trim[2:])
		}
		out = append(out, block{kind: kind, text: html.EscapeString(trim)})
	}
	return out
}

func renderAnswer(input string) []cv1.OutMessage {
	blocks := parseMarkdown(input)
	var lines []string
	for _, b := range blocks {
		switch b.kind {
		case "list":
			lines = append(lines, "• "+b.text)
		case "quote":
			lines = append(lines, "> "+b.text)
		default:
			lines = append(lines, b.text)
		}
	}
	return splitAnswer(strings.Join(lines, "\n"))
}

const maxTextPart = 4096
const maxTextParts = 5

func splitAnswer(answer string) []cv1.OutMessage {
	runes := []rune(answer)
	var out []cv1.OutMessage
	marker := []rune(locale.T(locale.EN, "channel.truncated"))
	for len(runes) > 0 && len(out) < maxTextParts {
		limit := maxTextPart
		if len(out) == maxTextParts-1 && len(runes) > maxTextPart {
			limit -= len(marker)
		}
		if limit > len(runes) {
			limit = len(runes)
		}
		part := string(runes[:limit])
		runes = runes[limit:]
		if len(out) == maxTextParts-1 && len(runes) > 0 {
			part += string(marker)
		}
		out = append(out, cv1.OutMessage{Kind: "text", Body: part})
	}
	if len(runes) > 0 {
		out = append(out, cv1.OutMessage{Kind: "file", Body: string(runes)})
	}
	if len(out) == 0 {
		out = append(out, cv1.OutMessage{Kind: "text", Body: ""})
	}
	return out
}
