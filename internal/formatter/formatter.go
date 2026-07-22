package formatter

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Options struct {
	TabSize      int
	InsertSpaces bool
}

type segmentKind uint8

const (
	segmentCode segmentKind = iota
	segmentQuoted
	segmentLineComment
	segmentBlockComment
)

type segment struct {
	Kind segmentKind
	Text string
}

type blockContext struct {
	Switch     bool
	CaseActive bool
}

type hangingContext struct {
	ParenBase   int
	BracketBase int
	AngleBase   int
	Indent      int
}

type formatState struct {
	Blocks            []blockContext
	ParenDepth        int
	BracketDepth      int
	AngleDepth        int
	InBlockComment    bool
	ContinuationDepth int
	Hanging           []hangingContext

	BlockCommentInputPrefix  string
	BlockCommentOutputPrefix string
}

func Format(text string, options Options) string {
	if text == "" {
		return ""
	}

	newline := "\n"
	if strings.Contains(text, "\r\n") {
		newline = "\r\n"
	}

	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	lines := strings.Split(normalized, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	indent := indentation(options)
	state := formatState{}
	formatted := make([]string, 0, len(lines))
	blank := false

	for _, rawLine := range lines {
		line := strings.TrimRightFunc(rawLine, unicode.IsSpace)

		/*
			Whitespace inside an already-open block comment is comment content,
			not source indentation. Preserve it exactly, apart from trailing
			whitespace, and continue scanning after a closing delimiter.
		*/
		if state.InBlockComment {
			segments, endsInBlockComment := splitSegments(line, true)
			mask := codeMask(segments)
			rebased := rebaseBlockCommentLine(
				line,
				state.BlockCommentInputPrefix,
				state.BlockCommentOutputPrefix,
			)
			formatted = append(formatted, rebased)
			blank = rebased == ""
			updateState(&state, mask)
			popClosedHangingContexts(&state)
			if strings.TrimSpace(mask) != "" {
				state.ContinuationDepth = nextContinuationDepth(
					state.ContinuationDepth,
					rebased,
				)
			}
			state.InBlockComment = endsInBlockComment
			if !endsInBlockComment {
				state.BlockCommentInputPrefix = ""
				state.BlockCommentOutputPrefix = ""
			}
			continue
		}

		trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)

		if trimmed == "" {
			if len(formatted) > 0 && !blank {
				formatted = append(formatted, "")
				blank = true
			}
			continue
		}

		segments, endsInBlockComment := splitSegments(trimmed, state.InBlockComment)
		mask := codeMask(segments)
		code := strings.TrimSpace(mask)

		virtualBlocks := state.Blocks
		closingBraces := leadingDelimiterCount(code, '}')
		if closingBraces > len(virtualBlocks) {
			closingBraces = len(virtualBlocks)
		}
		virtualBlocks = virtualBlocks[:len(virtualBlocks)-closingBraces]

		caseSwitch := -1
		if isCaseLabel(code) {
			caseSwitch = nearestSwitch(virtualBlocks)
		}

		indentLevel := len(virtualBlocks)
		for i := range virtualBlocks {
			if virtualBlocks[i].Switch && virtualBlocks[i].CaseActive && i != caseSwitch {
				indentLevel++
			}
		}
		for _, hanging := range state.Hanging {
			indentLevel += hanging.Indent
		}

		virtualParens := state.ParenDepth - leadingDelimiterCount(code, ')')
		virtualBrackets := state.BracketDepth - leadingDelimiterCount(code, ']')
		virtualAngles := state.AngleDepth - leadingDelimiterCount(code, '>')
		if virtualParens < 0 {
			virtualParens = 0
		}
		if virtualBrackets < 0 {
			virtualBrackets = 0
		}
		if virtualAngles < 0 {
			virtualAngles = 0
		}

		indentLevel += virtualParens + virtualBrackets + virtualAngles
		if virtualParens == 0 && virtualBrackets == 0 && virtualAngles == 0 && state.ContinuationDepth > 0 && !startsWithClosingContinuation(code) {
			indentLevel += state.ContinuationDepth
		}

		body := formatSegments(segments)
		indentPrefix := strings.Repeat(indent, indentLevel)
		formatted = append(formatted, indentPrefix+body)
		blank = false

		parenBefore := state.ParenDepth
		bracketBefore := state.BracketDepth
		angleBefore := state.AngleDepth
		continuationIndent := state.ContinuationDepth
		continuedIntoLine := continuationIndent > 0 && virtualParens == 0 && virtualBrackets == 0 && virtualAngles == 0 && !startsWithClosingContinuation(code)

		updateState(&state, mask)
		if continuedIntoLine && (state.ParenDepth > parenBefore || state.BracketDepth > bracketBefore || state.AngleDepth > angleBefore) {
			state.Hanging = append(state.Hanging, hangingContext{
				ParenBase:   parenBefore,
				BracketBase: bracketBefore,
				AngleBase:   angleBefore,
				Indent:      continuationIndent,
			})
		}
		popClosedHangingContexts(&state)

		if endsInBlockComment {
			state.BlockCommentInputPrefix = line[:len(line)-len(trimmed)]
			state.BlockCommentOutputPrefix = indentPrefix
		}

		if isCaseLabel(code) {
			if index := nearestSwitch(state.Blocks); index >= 0 {
				state.Blocks[index].CaseActive = true
			}
		}
		if code != "" {
			state.ContinuationDepth = nextContinuationDepth(
				state.ContinuationDepth,
				body,
			)
		}
		state.InBlockComment = endsInBlockComment
	}

	for len(formatted) > 0 && formatted[len(formatted)-1] == "" {
		formatted = formatted[:len(formatted)-1]
	}

	if len(formatted) == 0 {
		return ""
	}

	return strings.Join(formatted, newline) + newline
}

func indentation(options Options) string {
	if !options.InsertSpaces {
		return "\t"
	}

	width := options.TabSize
	if width <= 0 {
		width = 4
	}

	return strings.Repeat(" ", width)
}

func splitSegments(line string, inBlockComment bool) ([]segment, bool) {
	var segments []segment
	start := 0
	index := 0

	flushCode := func(end int) {
		if end > start {
			segments = append(segments, segment{Kind: segmentCode, Text: line[start:end]})
		}
	}

	for index < len(line) {
		if inBlockComment {
			end := strings.Index(line[index:], "*/")
			if end < 0 {
				segments = append(segments, segment{Kind: segmentBlockComment, Text: line[index:]})
				return segments, true
			}

			end += index + 2
			segments = append(segments, segment{Kind: segmentBlockComment, Text: line[index:end]})
			index = end
			start = index
			inBlockComment = false
			continue
		}

		if index+1 < len(line) {
			switch line[index : index+2] {
			case "//":
				flushCode(index)
				segments = append(segments, segment{Kind: segmentLineComment, Text: line[index:]})
				return segments, false

			case "/*":
				flushCode(index)
				end := strings.Index(line[index+2:], "*/")
				if end < 0 {
					segments = append(segments, segment{Kind: segmentBlockComment, Text: line[index:]})
					return segments, true
				}

				end += index + 4
				segments = append(segments, segment{Kind: segmentBlockComment, Text: line[index:end]})
				index = end
				start = index
				continue
			}
		}

		if line[index] == '"' || line[index] == '\'' {
			flushCode(index)
			quote := line[index]
			end := index + 1

			for end < len(line) {
				if line[end] == '\\' {
					end++
					if end < len(line) {
						_, width := utf8.DecodeRuneInString(line[end:])
						end += width
					}
					continue
				}

				if line[end] == quote {
					end++
					break
				}

				_, width := utf8.DecodeRuneInString(line[end:])
				end += width
			}

			segments = append(segments, segment{Kind: segmentQuoted, Text: line[index:end]})
			index = end
			start = index
			continue
		}

		_, width := utf8.DecodeRuneInString(line[index:])
		index += width
	}

	flushCode(len(line))
	return segments, inBlockComment
}

func codeMask(segments []segment) string {
	var builder strings.Builder

	for _, item := range segments {
		if item.Kind == segmentCode {
			builder.WriteString(item.Text)
			continue
		}

		builder.WriteString(strings.Repeat(" ", len(item.Text)))
	}

	return builder.String()
}

func formatSegments(segments []segment) string {
	var builder strings.Builder
	pendingSpace := false

	for _, item := range segments {
		switch item.Kind {
		case segmentCode:
			appendNormalizedCode(&builder, item.Text, &pendingSpace)

		case segmentLineComment, segmentBlockComment:
			trimBuilderRightSpace(&builder)
			if builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			builder.WriteString(item.Text)
			pendingSpace = false

		case segmentQuoted:
			writePendingSpace(&builder, pendingSpace)
			builder.WriteString(item.Text)
			pendingSpace = false
		}
	}

	trimBuilderRightSpace(&builder)
	return strings.TrimSpace(builder.String())
}

var spacedOperators = []string{
	"::", ":=", "==", "!=", "<=", ">=", "&&", "||",
	"+=", "-=", "*=", "/=", "%=", "=",
}

func appendNormalizedCode(builder *strings.Builder, code string, pendingSpace *bool) {
	for index := 0; index < len(code); {
		r, width := utf8.DecodeRuneInString(code[index:])
		if unicode.IsSpace(r) {
			*pendingSpace = true
			index += width
			continue
		}

		if operator := operatorAt(code[index:]); operator != "" {
			writeSpacedOperator(builder, operator)
			*pendingSpace = true
			index += len(operator)
			continue
		}

		switch r {
		case ',', ';':
			trimBuilderRightSpace(builder)
			builder.WriteRune(r)
			*pendingSpace = true

		case '.':
			if shouldPreserveSpaceBeforeDot(builder.String(), *pendingSpace) {
				trimBuilderRightSpace(builder)
				builder.WriteByte(' ')
			} else {
				trimBuilderRightSpace(builder)
			}
			builder.WriteRune(r)
			*pendingSpace = false

		case ':':
			trimBuilderRightSpace(builder)
			builder.WriteRune(r)
			*pendingSpace = true

		case '(', '[':
			preserveSpace := r == '(' && shouldPreserveSpaceBeforeOpenParen(builder.String(), *pendingSpace)
			trimBuilderRightSpace(builder)
			if preserveSpace {
				builder.WriteByte(' ')
			}
			builder.WriteRune(r)
			*pendingSpace = false

		case ')', ']':
			trimBuilderRightSpace(builder)
			builder.WriteRune(r)
			*pendingSpace = false

		case '@':
			writePendingSpace(builder, *pendingSpace)
			builder.WriteRune(r)
			*pendingSpace = false

		case '{':
			if shouldSpaceBeforeBrace(builder.String()) {
				trimBuilderRightSpace(builder)
				if builder.Len() > 0 {
					builder.WriteByte(' ')
				}
			} else {
				writePendingSpace(builder, *pendingSpace)
			}
			builder.WriteRune(r)
			*pendingSpace = false

		case '}':
			trimBuilderRightSpace(builder)
			builder.WriteRune(r)
			*pendingSpace = true

		default:
			writePendingSpace(builder, *pendingSpace)
			builder.WriteRune(r)
			*pendingSpace = false
		}

		index += width
	}
}

func shouldPreserveSpaceBeforeOpenParen(prefix string, pending bool) bool {
	if !pending {
		return false
	}

	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}

	last, _ := utf8.DecodeLastRuneInString(prefix)
	return last == ')' || last == ']' || last == '>'
}

func shouldPreserveSpaceBeforeDot(prefix string, pending bool) bool {
	if !pending {
		return false
	}

	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}

	last, _ := utf8.DecodeLastRuneInString(prefix)
	if last == ',' || last == '=' || last == '(' || last == '[' || last == '{' || last == ':' {
		return true
	}

	for _, operator := range []string{"==", "!=", "<=", ">=", "&&", "||", "+", "-", "*", "/", "%", "&", "|", "^", "<", ">"} {
		if strings.HasSuffix(prefix, operator) {
			return true
		}
	}

	words := strings.FieldsFunc(prefix, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	})
	if len(words) == 0 {
		return false
	}

	switch words[len(words)-1] {
	case "return", "case":
		return true
	default:
		return false
	}
}

func operatorAt(text string) string {
	for _, operator := range spacedOperators {
		if strings.HasPrefix(text, operator) {
			return operator
		}
	}
	return ""
}

func writeSpacedOperator(builder *strings.Builder, operator string) {
	trimBuilderRightSpace(builder)
	if builder.Len() > 0 {
		last, _ := utf8.DecodeLastRuneInString(builder.String())
		if last != '(' && last != '[' && last != '{' {
			builder.WriteByte(' ')
		}
	}
	builder.WriteString(operator)
}

func writePendingSpace(builder *strings.Builder, pending bool) {
	if !pending || builder.Len() == 0 {
		return
	}

	last, _ := utf8.DecodeLastRuneInString(builder.String())
	switch last {
	case '(', '[', '.', '@':
		return
	}

	builder.WriteByte(' ')
}

func trimBuilderRightSpace(builder *strings.Builder) {
	text := strings.TrimRightFunc(builder.String(), unicode.IsSpace)
	builder.Reset()
	builder.WriteString(text)
}

func shouldSpaceBeforeBrace(prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}

	last, _ := utf8.DecodeLastRuneInString(prefix)
	if last == ')' {
		return true
	}

	words := strings.FieldsFunc(prefix, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	})

	for _, word := range words {
		switch word {
		case "if", "else", "for", "switch", "task", "struct", "enum", "union", "interface", "impl", "overload", "defer":
			return true
		}
	}

	return false
}

func rebaseBlockCommentLine(line string, inputPrefix string, outputPrefix string) string {
	if line == "" {
		return ""
	}

	relative := line
	if inputPrefix != "" && strings.HasPrefix(relative, inputPrefix) {
		relative = relative[len(inputPrefix):]
	}

	return outputPrefix + relative
}

func popClosedHangingContexts(state *formatState) {
	for len(state.Hanging) > 0 {
		last := state.Hanging[len(state.Hanging)-1]
		if state.ParenDepth > last.ParenBase || state.BracketDepth > last.BracketBase || state.AngleDepth > last.AngleBase {
			return
		}
		state.Hanging = state.Hanging[:len(state.Hanging)-1]
	}
}

func startsWithClosingContinuation(code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}

	switch code[0] {
	case ')', ']', '}', '>':
		return true
	default:
		return false
	}
}

func nextContinuationDepth(current int, code string) int {
	code = strings.TrimSpace(code)
	if !lineContinues(code) {
		return 0
	}

	if current <= 0 {
		return 1
	}

	if endsComparisonOperator(code) {
		return current + 1
	}

	return current
}

func lineContinues(code string) bool {
	code = strings.TrimSpace(code)
	if code == "" || startsWithClosingContinuation(code) {
		return false
	}

	for _, suffix := range []string{
		"::", ":=", "==", "!=", "<=", ">=", "&&", "||", "<<", ">>",
		"+=", "-=", "*=", "/=", "%=", "=", "+", "-", "*", "/", "%",
		"&", "|", "^", "<", ">",
	} {
		if strings.HasSuffix(code, suffix) {
			return true
		}
	}

	return false
}

func endsComparisonOperator(code string) bool {
	code = strings.TrimSpace(code)
	for _, suffix := range []string{"==", "!=", "<=", ">=", "<", ">"} {
		if strings.HasSuffix(code, suffix) {
			return true
		}
	}
	return false
}

func leadingDelimiterCount(code string, delimiter byte) int {
	count := 0
	for index := 0; index < len(code); {
		if unicode.IsSpace(rune(code[index])) {
			index++
			continue
		}
		if code[index] != delimiter {
			break
		}
		count++
		index++
	}
	return count
}

func isCaseLabel(code string) bool {
	code = strings.TrimSpace(code)
	return strings.HasPrefix(code, "case ") || strings.HasPrefix(code, "case\t") || strings.HasPrefix(code, "default:")
}

func nearestSwitch(blocks []blockContext) int {
	for index := len(blocks) - 1; index >= 0; index-- {
		if blocks[index].Switch {
			return index
		}
	}
	return -1
}

func updateState(state *formatState, code string) {
	statementStart := 0

	for index := 0; index < len(code); index++ {
		switch code[index] {
		case '{':
			prefix := code[statementStart:index]
			state.Blocks = append(state.Blocks, blockContext{Switch: containsWord(prefix, "switch")})
			statementStart = index + 1

		case '}':
			if len(state.Blocks) > 0 {
				state.Blocks = state.Blocks[:len(state.Blocks)-1]
			}
			statementStart = index + 1

		case '(':
			state.ParenDepth++

		case ')':
			if state.ParenDepth > 0 {
				state.ParenDepth--
			}

		case '[':
			state.BracketDepth++

		case ']':
			if state.BracketDepth > 0 {
				state.BracketDepth--
			}

		case '<':
			if isGenericAngleOpen(code, index) {
				state.AngleDepth++
			}

		case '>':
			if state.AngleDepth > 0 && isGenericAngleClose(code, index) {
				state.AngleDepth--
			}
		}
	}
}

func isGenericAngleOpen(code string, index int) bool {
	if index < 0 || index >= len(code) || code[index] != '<' {
		return false
	}
	if index+1 < len(code) && (code[index+1] == '=' || code[index+1] == '<') {
		return false
	}

	previous := previousNonSpaceByte(code, index)
	if previous >= 0 && previous == index-1 {
		ch := code[previous]
		if isIdentifierByte(ch) || ch == ')' || ch == ']' || ch == '>' {
			return true
		}
	}

	prefix := strings.TrimSpace(code[:index])
	for _, keyword := range []string{"task", "struct", "interface", "union", "enum"} {
		if strings.HasSuffix(prefix, keyword) {
			return true
		}
	}

	return false
}

func isGenericAngleClose(code string, index int) bool {
	if index < 0 || index >= len(code) || code[index] != '>' {
		return false
	}
	if index+1 < len(code) && code[index+1] == '=' {
		return false
	}

	next := nextNonSpaceByte(code, index+1)
	if next < 0 {
		return true
	}

	ch := code[next]
	if isIdentifierByte(ch) || (ch >= '0' && ch <= '9') {
		return false
	}

	return true
}

func previousNonSpaceByte(text string, before int) int {
	for index := before - 1; index >= 0; index-- {
		if !unicode.IsSpace(rune(text[index])) {
			return index
		}
	}
	return -1
}

func nextNonSpaceByte(text string, start int) int {
	for index := start; index < len(text); index++ {
		if !unicode.IsSpace(rune(text[index])) {
			return index
		}
	}
	return -1
}

func isIdentifierByte(ch byte) bool {
	return ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
}

func containsWord(text string, wanted string) bool {
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	})
	for _, word := range words {
		if word == wanted {
			return true
		}
	}
	return false
}
