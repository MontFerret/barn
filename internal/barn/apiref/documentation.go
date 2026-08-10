package apiref

import (
	"go/ast"
	"go/token"
	"strings"
	"unicode"
)

type documentationBody struct {
	text      string
	positions []token.Pos
}

func normalizeDocumentation(group *ast.CommentGroup) documentationBody {
	if group == nil {
		return documentationBody{}
	}

	lines := make([]string, 0, len(group.List))
	positions := make([]token.Pos, 0, len(group.List))

	for _, comment := range group.List {
		text := comment.Text
		if strings.HasPrefix(text, "//") {
			content := strings.TrimPrefix(text, "//")
			offset := 2
			if strings.HasPrefix(content, " ") {
				content = content[1:]
				offset++
			}

			lines = append(lines, content)
			positions = append(positions, comment.Slash+token.Pos(offset))

			continue
		}

		content := strings.TrimPrefix(text, "/*")
		content = strings.TrimSuffix(content, "*/")
		offset := 2

		for raw := range strings.SplitAfterSeq(content, "\n") {
			line := strings.TrimSuffix(raw, "\n")
			lineOffset := strings.IndexFunc(line, func(character rune) bool {
				return character != ' ' && character != '\t'
			})
			if lineOffset < 0 {
				lineOffset = len(line)
			}

			normalized := line[lineOffset:]
			if strings.HasPrefix(normalized, "*") {
				lineOffset++
				normalized = normalized[1:]
				if strings.HasPrefix(normalized, " ") {
					lineOffset++
					normalized = normalized[1:]
				}
			}

			lines = append(lines, normalized)
			positions = append(positions, comment.Slash+token.Pos(offset+lineOffset))
			offset += len(raw)
		}
	}

	return documentationBody{text: strings.Join(lines, "\n"), positions: positions}
}

func (body documentationBody) position(line int) token.Pos {
	if line < 1 || line > len(body.positions) {
		return token.NoPos
	}

	return body.positions[line-1]
}

func (body documentationBody) annotationPosition(tag string) token.Pos {
	for index, line := range strings.Split(body.text, "\n") {
		if line == tag {
			return body.position(index + 1)
		}

		if strings.HasPrefix(line, tag) {
			rest := line[len(tag):]
			if rest != "" && unicode.IsSpace(rune(rest[0])) {
				return body.position(index + 1)
			}
		}
	}

	return token.NoPos
}
