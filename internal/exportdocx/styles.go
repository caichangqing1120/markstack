package exportdocx

import (
	"encoding/xml"
	"fmt"
	"strings"
)

const (
	listStep    = 420
	listHanging = 240
	listLevels  = 9
	monoFonts   = `<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/>`
	codeShade   = `<w:shd w:val="clear" w:color="auto" w:fill="F3F5F6"/>`
)

func renderStyles() string {
	var styles strings.Builder
	styles.WriteString(xml.Header + `<w:styles xmlns:w="` + wordNS + `">` +
		`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/>` +
		`<w:pPr><w:spacing w:after="140" w:line="276" w:lineRule="auto"/></w:pPr>` +
		`<w:rPr><w:sz w:val="22"/></w:rPr></w:style>`)
	for index, size := range []int{36, 32, 28, 26, 24, 22} {
		fmt.Fprintf(&styles, `<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/>`+
			`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/>`+
			`<w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="240" w:after="100"/><w:outlineLvl w:val="%d"/></w:pPr>`+
			`<w:rPr><w:b/><w:sz w:val="%d"/></w:rPr></w:style>`, index+1, index+1, index, size)
	}
	styles.WriteString(`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr><w:pBdr><w:left w:val="single" w:sz="18" w:space="8" w:color="76A2B6"/></w:pBdr>` +
		`<w:ind w:left="360"/></w:pPr><w:rPr><w:color w:val="58646A"/></w:rPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="CodeBlock"><w:name w:val="Code Block"/><w:basedOn w:val="Normal"/>` +
		`<w:pPr>` + codeShade + `<w:spacing w:before="120" w:after="180"/></w:pPr>` +
		`<w:rPr>` + monoFonts + `<w:sz w:val="20"/></w:rPr></w:style>` +
		`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/>` +
		`<w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>` +
		`<w:style w:type="character" w:styleId="InlineCode"><w:name w:val="Inline Code"/>` +
		`<w:rPr>` + monoFonts + codeShade + `</w:rPr></w:style></w:styles>`)
	return styles.String()
}

type listNumber struct {
	ordered bool
	level   int
	start   int32
}

func renderNumbering(lists []listNumber) *limitedBuffer {
	out := &limitedBuffer{}
	out.write(xml.Header + `<w:numbering xmlns:w="` + wordNS + `">`)
	for index, format := range []string{"bullet", "decimal"} {
		fmt.Fprintf(out, `<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="multilevel"/>`, index)
		for level := range listLevels {
			label := []string{"•", "◦", "▪"}[level%3]
			if format == "decimal" {
				label = fmt.Sprintf("%%%d.", level+1)
			}
			indent := (level + 1) * listStep
			fmt.Fprintf(out, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%s"/>`+
				`<w:lvlJc w:val="left"/><w:pPr><w:tabs><w:tab w:val="num" w:pos="%d"/></w:tabs>`+
				`<w:ind w:left="%d" w:hanging="%d"/></w:pPr></w:lvl>`, level, format, label, indent, indent, listHanging)
		}
		out.write(`</w:abstractNum>`)
	}
	for index, list := range lists {
		abstract := 0
		if list.ordered {
			abstract = 1
		}
		fmt.Fprintf(out, `<w:num w:numId="%d"><w:abstractNumId w:val="%d"/>`, index+1, abstract)
		if list.ordered && list.start != 1 {
			fmt.Fprintf(out, `<w:lvlOverride w:ilvl="%d"><w:startOverride w:val="%d"/></w:lvlOverride>`, list.level, list.start)
		}
		out.write(`</w:num>`)
		if out.err != nil {
			break
		}
	}
	out.write(`</w:numbering>`)
	return out
}
