package apiref

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
	"unicode"

	registryartifact "github.com/MontFerret/specs/pkg/registry/artifact"
)

type (
	documentationMetadata struct {
		description       string
		parameters        []registryartifact.APIParameter
		parameterPosition token.Pos
		returnValue       *registryartifact.APIReturn
		throws            []registryartifact.APIThrownError
		deprecated        string
	}

	documentationLine struct {
		text     string
		position token.Pos
	}

	documentationParseError struct {
		position token.Pos
		message  string
	}
)

const (
	parameterAnnotation  = "@param"
	returnAnnotation     = "@return"
	throwsAnnotation     = "@throws"
	deprecatedAnnotation = "@deprecated"
)

func (e *documentationParseError) Error() string {
	return e.message
}

func parseDocumentation(declaration *ast.FuncDecl) (documentationMetadata, error) {
	metadata := documentationMetadata{}

	if declaration == nil || declaration.Doc == nil {
		return metadata, nil
	}

	lines := documentationLines(declaration.Doc)
	prose := make([]documentationLine, 0, len(lines))
	parameterNames := make(map[string]struct{})

	for _, line := range lines {
		annotation := line.text

		tag, exists := supportedAnnotation(annotation)
		if !exists {
			prose = append(prose, line)

			continue
		}

		switch tag {
		case parameterAnnotation:
			parameter, err := parseParameterAnnotation(declaration.Name.Name, line, annotation)
			if err != nil {
				return documentationMetadata{}, err
			}

			if _, exists := parameterNames[parameter.Name]; exists {
				return documentationMetadata{}, annotationError(
					declaration.Name.Name,
					line,
					tag,
					annotation,
					fmt.Sprintf("parameter %q is declared more than once", parameter.Name),
				)
			}
			parameterNames[parameter.Name] = struct{}{}

			if len(metadata.parameters) == 0 {
				metadata.parameterPosition = line.position
			}

			metadata.parameters = append(metadata.parameters, parameter)
		case returnAnnotation:
			if metadata.returnValue != nil {
				return documentationMetadata{}, annotationError(
					declaration.Name.Name,
					line,
					tag,
					annotation,
					"a declaration may contain at most one @return",
				)
			}

			value, description, err := parseTypedAnnotation(declaration.Name.Name, line, tag, annotation)
			if err != nil {
				return documentationMetadata{}, err
			}

			metadata.returnValue = &registryartifact.APIReturn{Type: value, Description: description}
		case throwsAnnotation:
			value, description, err := parseTypedAnnotation(declaration.Name.Name, line, tag, annotation)
			if err != nil {
				return documentationMetadata{}, err
			}

			metadata.throws = append(metadata.throws, registryartifact.APIThrownError{Error: value, Description: description})
		case deprecatedAnnotation:
			if metadata.deprecated != "" {
				return documentationMetadata{}, annotationError(
					declaration.Name.Name,
					line,
					tag,
					annotation,
					"a declaration may contain at most one @deprecated",
				)
			}

			description := strings.TrimSpace(strings.TrimPrefix(annotation, tag))
			if description == "" {
				return documentationMetadata{}, annotationError(
					declaration.Name.Name,
					line,
					tag,
					annotation,
					`expected "@deprecated <description>"`,
				)
			}

			metadata.deprecated = description
		}
	}

	metadata.description = documentationProse(prose)

	if metadata.deprecated != "" {
		metadata.description = removeStandardDeprecation(metadata.description)
	}

	return metadata, nil
}

func parseParameterAnnotation(declarationName string, line documentationLine, annotation string) (registryartifact.APIParameter, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(annotation, parameterAnnotation))

	name, rest, ok := cutAnnotationToken(rest)
	if !ok || !validParameterName(name) || name == "_" {
		return registryartifact.APIParameter{}, annotationError(
			declarationName,
			line,
			parameterAnnotation,
			annotation,
			`expected "@param <name> {<type>} <description>"`,
		)
	}

	value, description, reason := parseAnnotationValue(rest)
	if reason != "" {
		return registryartifact.APIParameter{}, annotationError(
			declarationName,
			line,
			parameterAnnotation,
			annotation,
			fmt.Sprintf(`expected "@param <name> {<type>} <description>": %s`, reason),
		)
	}

	return registryartifact.APIParameter{Name: name, Type: value, Description: description}, nil
}

func parseTypedAnnotation(declarationName string, line documentationLine, tag, annotation string) (string, string, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(annotation, tag))

	value, description, reason := parseAnnotationValue(rest)
	if reason != "" {
		expected := fmt.Sprintf(`expected "%s {<type>} <description>"`, tag)
		if tag == throwsAnnotation {
			expected = `expected "@throws {<error>} <description>"`
		}

		return "", "", annotationError(
			declarationName,
			line,
			tag,
			annotation,
			fmt.Sprintf("%s: %s", expected, reason),
		)
	}

	return value, description, nil
}

func parseAnnotationValue(value string) (string, string, string) {
	if value == "" || value[0] != '{' {
		return "", "", "annotation value must begin with an opening brace"
	}

	depth := 0
	closing := -1
	for index, character := range value {
		switch character {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closing = index

				break
			}
		}

		if closing >= 0 {
			break
		}
	}

	if closing < 0 {
		return "", "", "annotation value is missing a closing brace"
	}

	typeExpression := value[1:closing]
	if strings.TrimSpace(typeExpression) == "" {
		return "", "", "annotation value must not be blank"
	}

	rest := value[closing+1:]
	if rest == "" || !unicode.IsSpace(rune(rest[0])) {
		return "", "", "annotation value must be followed by a description"
	}

	description := strings.TrimSpace(rest)
	if description == "" {
		return "", "", "annotation description must not be blank"
	}

	if description == "-" || strings.HasPrefix(description, "- ") {
		return "", "", "annotation description must not use a JSDoc '-' separator"
	}

	return typeExpression, description, ""
}

func supportedAnnotation(line string) (string, bool) {
	for _, tag := range []string{parameterAnnotation, returnAnnotation, throwsAnnotation, deprecatedAnnotation} {
		if line == tag {
			return tag, true
		}

		if strings.HasPrefix(line, tag) {
			rest := line[len(tag):]
			if rest != "" && unicode.IsSpace(rune(rest[0])) {
				return tag, true
			}
		}
	}

	return "", false
}

func cutAnnotationToken(value string) (string, string, bool) {
	index := strings.IndexFunc(value, unicode.IsSpace)
	if index <= 0 {
		return "", "", false
	}

	return value[:index], strings.TrimSpace(value[index:]), true
}

func validParameterName(value string) bool {
	if value == "" || !asciiLetter(value[0]) && value[0] != '_' {
		return false
	}

	for index := 1; index < len(value); index++ {
		if !asciiLetter(value[index]) && (value[index] < '0' || value[index] > '9') && value[index] != '_' {
			return false
		}
	}

	return true
}

func asciiLetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func documentationLines(group *ast.CommentGroup) []documentationLine {
	lines := make([]documentationLine, 0, len(group.List))

	for _, comment := range group.List {
		text := comment.Text
		if strings.HasPrefix(text, "//") {
			content := strings.TrimPrefix(text, "//")
			offset := 2
			if strings.HasPrefix(content, " ") {
				content = content[1:]
				offset++
			}

			lines = append(lines, documentationLine{text: content, position: comment.Slash + token.Pos(offset)})

			continue
		}

		content := strings.TrimPrefix(text, "/*")
		content = strings.TrimSuffix(content, "*/")
		offset := 2

		for _, raw := range strings.SplitAfter(content, "\n") {
			line := strings.TrimSuffix(raw, "\n")

			lineOffset := strings.IndexFunc(line, func(character rune) bool { return character != ' ' && character != '\t' })
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

			lines = append(lines, documentationLine{
				text:     normalized,
				position: comment.Slash + token.Pos(offset+lineOffset),
			})
			offset += len(raw)
		}
	}

	return lines
}

func documentationProse(lines []documentationLine) string {
	if len(lines) == 0 {
		return ""
	}

	comments := make([]*ast.Comment, 0, len(lines))
	for _, line := range lines {
		if line.text == "" {
			comments = append(comments, &ast.Comment{Text: "//"})

			continue
		}

		comments = append(comments, &ast.Comment{Text: "// " + line.text})
	}

	return strings.TrimSpace((&ast.CommentGroup{List: comments}).Text())
}

func removeStandardDeprecation(description string) string {
	paragraphs := strings.Split(description, "\n\n")
	kept := paragraphs[:0]

	for _, paragraph := range paragraphs {
		if strings.HasPrefix(strings.TrimSpace(paragraph), "Deprecated:") {
			continue
		}

		kept = append(kept, paragraph)
	}

	return strings.TrimSpace(strings.Join(kept, "\n\n"))
}

func annotationError(declarationName string, line documentationLine, tag, annotation, reason string) error {
	return &documentationParseError{
		position: line.position,
		message:  fmt.Sprintf("%s: malformed %s %q: %s", declarationName, tag, annotation, reason),
	}
}
