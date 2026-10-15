package ui

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	mdtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type nativeMarkdownStyle struct {
	size                       float64
	bold, italic, code, strike bool
	link                       string
}

type nativeMarkdownSpan struct {
	value string
	style nativeMarkdownStyle
}

type nativeMarkdownBlock struct {
	spans                     []nativeMarkdownSpan
	indent                    int
	marker, anchor            string
	code, quote, rule, notice bool
	table                     *nativeMarkdownTable
}

type nativeMarkdownTable struct {
	alignments []extast.Alignment
	rows       [][][]nativeMarkdownSpan
}

// Preserve TeX punctuation as a literal span so the CommonMark parser cannot
// turn underscores/asterisks inside a formula into emphasis in fallback mode.
type nativeMathLiteralParser struct{}

func (*nativeMathLiteralParser) Trigger() []byte { return []byte{'$', '\\'} }

func (*nativeMathLiteralParser) Parse(_ ast.Node, reader mdtext.Reader, _ parser.Context) ast.Node {
	line, _ := reader.PeekLine()
	opening, closing := "", ""
	switch {
	case bytes.HasPrefix(line, []byte("$$")):
		opening, closing = "$$", "$$"
	case bytes.HasPrefix(line, []byte("$`")):
		opening, closing = "$`", "`$"
	case bytes.HasPrefix(line, []byte("$")):
		opening, closing = "$", "$"
	case bytes.HasPrefix(line, []byte(`\(`)):
		opening, closing = `\(`, `\)`
	case bytes.HasPrefix(line, []byte(`\[`)):
		opening, closing = `\[`, `\]`
	default:
		return nil
	}
	savedLine, savedPosition := reader.Position()
	reader.Advance(len(opening))
	var literal strings.Builder
	literal.WriteString(opening)
	for {
		line, _ = reader.PeekLine()
		if line == nil {
			reader.SetPosition(savedLine, savedPosition)
			return nil
		}
		search := 0
		for search < len(line) {
			match := bytes.Index(line[search:], []byte(closing))
			if match < 0 {
				break
			}
			match += search
			backslashes := 0
			for previous := match - 1; previous >= 0 && line[previous] == '\\'; previous-- {
				backslashes++
			}
			if backslashes%2 == 0 {
				literal.Write(line[:match+len(closing)])
				reader.Advance(match + len(closing))
				return ast.NewString([]byte(literal.String()))
			}
			search = match + len(closing)
		}
		literal.Write(line)
		reader.AdvanceLine()
	}
}

// Parsing is pure Go and runs on the background worker that handles browser
// failures. The fallback never loads scripts, images or browser resources.
func parseNativeMarkdown(value, baseURL string) []nativeMarkdownBlock {
	source := []byte(value)
	document := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Footnote, extension.DefinitionList), goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithInlineParsers(util.Prioritized(&nativeMathLiteralParser{}, 50)))).Parser().Parse(mdtext.NewReader(source))
	var result []nativeMarkdownBlock
	unescape := func(raw []byte) string { return html.UnescapeString(string(util.UnescapePunctuations(raw))) }
	base, _ := url.Parse(baseURL)
	resolve := func(raw []byte) string {
		value := unescape(raw)
		if strings.HasPrefix(value, "#") {
			return value
		}
		link, err := url.Parse(value)
		if err != nil {
			return ""
		}
		if base != nil {
			link = base.ResolveReference(link)
		}
		if link.Scheme == "mailto" {
			return link.String()
		}
		if (link.Scheme != "https" && link.Scheme != "http") || link.Hostname() == "" || link.User != nil {
			return ""
		}
		return link.String()
	}
	var inline func(ast.Node, nativeMarkdownStyle, *[]nativeMarkdownSpan)
	inline = func(node ast.Node, style nativeMarkdownStyle, spans *[]nativeMarkdownSpan) {
		add := func(value string) { *spans = append(*spans, nativeMarkdownSpan{value: value, style: style}) }
		switch n := node.(type) {
		case *ast.Text:
			value := string(n.Segment.Value(source))
			if !n.IsRaw() {
				value = unescape(n.Segment.Value(source))
			}
			add(value)
			if n.HardLineBreak() {
				add("\n")
			} else if n.SoftLineBreak() {
				add(" ")
			}
			return
		case *ast.String:
			add(string(n.Value))
			return
		case *ast.CodeSpan:
			style.code = true
			var value strings.Builder
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				if t, ok := child.(*ast.Text); ok {
					value.Write(t.Segment.Value(source))
				}
			}
			code := strings.ReplaceAll(value.String(), "\n", " ")
			if strings.HasPrefix(code, " ") && strings.HasSuffix(code, " ") && strings.TrimSpace(code) != "" {
				code = code[1 : len(code)-1]
			}
			add(code)
			return
		case *ast.Emphasis:
			if n.Level == 2 {
				style.bold = true
			} else {
				style.italic = true
			}
		case *extast.Strikethrough:
			style.strike = true
		case *ast.Link:
			style.link = resolve(n.Destination)
		case *ast.AutoLink:
			style.link = resolve(n.URL(source))
			add(string(n.Label(source)))
			return
		case *ast.Image:
			style.link = resolve(n.Destination)
			add("[图片] ")
		case *extast.TaskCheckBox:
			if n.IsChecked {
				add("☑ ")
			} else {
				add("☐ ")
			}
			return
		case *extast.FootnoteLink:
			style.link = fmt.Sprintf("#fn%d", n.Index)
			add(fmt.Sprintf("[%d]", n.Index))
			return
		case *extast.FootnoteBacklink:
			return
		case *ast.RawHTML:
			for i := 0; i < n.Segments.Len(); i++ {
				segment := n.Segments.At(i)
				raw := string(segment.Value(source))
				if strings.EqualFold(strings.TrimSpace(raw), "<br>") || strings.EqualFold(strings.TrimSpace(raw), "<br/>") || strings.EqualFold(strings.TrimSpace(raw), "<br />") {
					add("\n")
				} else {
					add(raw)
				}
			}
			return
		}
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			inline(child, style, spans)
		}
	}
	var blocks func(ast.Node, int, bool)
	blocks = func(node ast.Node, indent int, quote bool) {
		block := nativeMarkdownBlock{indent: indent, quote: quote}
		style := nativeMarkdownStyle{}
		switch n := node.(type) {
		case *ast.Heading:
			style.size = float64(max(15, 24-2*n.Level))
			style.bold = true
		case *ast.Paragraph, *ast.TextBlock:
		case *extast.DefinitionTerm:
			style.bold = true
		case *extast.DefinitionDescription:
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				blocks(child, indent+20, quote)
			}
			return
		case *extast.Table:
			table := &nativeMarkdownTable{alignments: append([]extast.Alignment(nil), n.Alignments...)}
			for row := n.FirstChild(); row != nil; row = row.NextSibling() {
				var cells [][]nativeMarkdownSpan
				_, header := row.(*extast.TableHeader)
				for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
					var spans []nativeMarkdownSpan
					inline(cell, nativeMarkdownStyle{bold: header}, &spans)
					cells = append(cells, spans)
				}
				table.rows = append(table.rows, cells)
			}
			block.table = table
			result = append(result, block)
			return
		case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock:
			block.code = true
			style.code = true
			var value strings.Builder
			if fenced, ok := node.(*ast.FencedCodeBlock); ok {
				language := strings.ToLower(string(fenced.Language(source)))
				if language == "mermaid" || language == "math" {
					value.WriteString(language + "（原生模式显示原文）\n")
				}
			}
			lines := node.Lines()
			for i := 0; i < lines.Len(); i++ {
				segment := lines.At(i)
				value.Write(segment.Value(source))
			}
			if raw, ok := node.(*ast.HTMLBlock); ok && raw.HasClosure() {
				value.Write(raw.ClosureLine.Value(source))
			}
			block.spans = []nativeMarkdownSpan{{value: strings.TrimSuffix(value.String(), "\n"), style: style}}
			result = append(result, block)
			return
		case *ast.ThematicBreak:
			block.rule = true
			result = append(result, block)
			return
		case *ast.Blockquote:
			start := len(result)
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				blocks(child, indent+12, true)
			}
			if len(result) > start && len(result[start].spans) > 0 {
				first := &result[start].spans[0]
				for _, kind := range []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"} {
					if strings.HasPrefix(first.value, "[!"+kind+"]") {
						first.value = kind + strings.TrimPrefix(first.value, "[!"+kind+"]")
						first.style.bold = true
						break
					}
				}
			}
			return
		case *ast.List:
			index := n.Start
			for item := n.FirstChild(); item != nil; item = item.NextSibling() {
				start := len(result)
				blocks(item, indent+24, quote)
				if len(result) > start {
					marker := "• "
					if n.IsOrdered() {
						marker = fmt.Sprintf("%d. ", index)
						index++
					}
					result[start].marker = marker
				}
			}
			return
		case *extast.Footnote:
			start := len(result)
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				blocks(child, indent+24, quote)
			}
			if len(result) > start {
				result[start].marker = fmt.Sprintf("%d. ", n.Index)
				result[start].anchor = fmt.Sprintf("fn%d", n.Index)
			}
			return
		case *extast.FootnoteList:
			result = append(result, nativeMarkdownBlock{rule: true})
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				blocks(child, indent, quote)
			}
			return
		default:
			for child := node.FirstChild(); child != nil; child = child.NextSibling() {
				blocks(child, indent, quote)
			}
			return
		}
		inline(node, style, &block.spans)
		if _, heading := node.(*ast.Heading); heading {
			if id, ok := node.AttributeString("id"); ok {
				if value, ok := id.([]byte); ok {
					block.anchor = string(value)
				}
			}
		}
		result = append(result, block)
	}
	blocks(document, 0, false)
	return result
}
