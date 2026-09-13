#!/usr/bin/env python3
"""Build the two review PDFs from their versioned Markdown sources.

Requires reportlab and a Unicode TrueType font with Chinese glyphs.
Example: python scripts/build_rhino_submission.py --font /path/to/font.ttf
"""
import argparse
import html
from pathlib import Path
import re
from urllib.parse import quote

from reportlab.lib import colors
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.platypus import (SimpleDocTemplate, Paragraph, Spacer, Table,
                               TableStyle, KeepTogether)

ROOT = Path(__file__).resolve().parents[1]
SUBMISSION_REF = "rhino-2026-final-3-v2"
SUBMISSION_DATE = "2026-09-14"


def inline(value, source):
    value = value.replace("—", "-").replace("–", "-")
    parts = re.split(r"(`[^`]+`|\[[^\]]+\]\([^)]+\)|https?://[^\s<>]+)", value)
    out = []
    for part in parts:
        if part.startswith("`") and part.endswith("`"):
            out.append('<font color="#263B4B">' + html.escape(part[1:-1]) + '</font>')
        elif re.fullmatch(r"\[[^\]]+\]\([^)]+\)", part):
            label, target = re.match(r"\[([^\]]+)\]\(([^)]+)\)", part).groups()
            if not target.startswith("http"):
                relative = (source.parent / target).resolve().relative_to(ROOT).as_posix()
                ref = "codex/evaluation-four-dimension-results" if relative == "submission.yaml" else SUBMISSION_REF
                kind = "tree" if (ROOT / relative).is_dir() else "blob"
                target = f"https://github.com/yushangcan/WeKnora/{kind}/{ref}/" + quote(relative)
            out.append('<link href="' + html.escape(target, quote=True) + '" color="#175F83">' + html.escape(label) + '</link>')
        elif part.startswith(("https://", "http://")):
            out.append('<link href="' + html.escape(part, quote=True) + '" color="#175F83">' + html.escape(part) + '</link>')
        else:
            escaped = html.escape(part)
            escaped = re.sub(r"\*\*(.+?)\*\*", r"<b>\1</b>", escaped)
            out.append(escaped)
    return ''.join(out)


class ReviewPDF(SimpleDocTemplate):
    def afterFlowable(self, flowable):
        level = getattr(flowable, 'outline_level', None)
        if level is not None:
            title = flowable.getPlainText()
            key = f"section-{self.seq.nextf('section')}"
            self.canv.bookmarkPage(key)
            self.canv.addOutlineEntry(title, key, level, closed=False)


def build(source, output, font):
    body = ParagraphStyle('Body', fontName=font, fontSize=11, leading=18,
                          wordWrap='CJK', spaceAfter=7, allowWidows=0, allowOrphans=0)
    styles = {
        'body': body,
        'title': ParagraphStyle('Title', parent=body, fontSize=23, leading=33, spaceAfter=17, textColor=colors.black),
        'h1': ParagraphStyle('Heading1', parent=body, fontSize=16, leading=23, spaceBefore=17, spaceAfter=9,
                            keepWithNext=True, textColor=colors.HexColor('#163A50')),
        'h2': ParagraphStyle('Heading2', parent=body, fontSize=12.7, leading=19, spaceBefore=12, spaceAfter=7,
                            keepWithNext=True, textColor=colors.HexColor('#163A50')),
        'table': ParagraphStyle('Table', parent=body, fontSize=9.6, leading=14.2, spaceAfter=0),
        'th': ParagraphStyle('TableHeader', parent=body, fontSize=10, leading=15, spaceAfter=0,
                            textColor=colors.white),
        'code': ParagraphStyle('Code', parent=body, fontSize=9.5, leading=14.5, leftIndent=12, spaceAfter=3,
                              textColor=colors.HexColor('#263B4B')),
        'list': ParagraphStyle('List', parent=body, leftIndent=12, firstLineIndent=-8, spaceAfter=5),
    }
    lines = source.read_text(encoding='utf-8').splitlines()
    story = []
    i = 0
    width = A4[0] - 108
    while i < len(lines):
        line = lines[i].strip()
        if not line:
            i += 1
            continue
        if line.startswith('```'):
            i += 1
            code = []
            while i < len(lines) and not lines[i].strip().startswith('```'):
                code.append(Paragraph(html.escape(lines[i]).replace(' ', '&nbsp;'), styles['code']))
                i += 1
            story.append(KeepTogether(code + [Spacer(1, 6)]))
            i += 1
            continue
        if line.startswith('|'):
            rows = []
            while i < len(lines) and lines[i].strip().startswith('|'):
                cells = [c.strip() for c in lines[i].strip().strip('|').split('|')]
                if not all(re.fullmatch(r':?-+:?', c) for c in cells):
                    sty = styles['th'] if not rows else styles['table']
                    rows.append([Paragraph(inline(c, source), sty) for c in cells])
                i += 1
            ratios = {
                2: [0.27, 0.73], 3: [0.26, 0.36, 0.38],
                4: [0.18, 0.26, 0.28, 0.28],
                7: [0.18, 0.09, 0.17, 0.12, 0.12, 0.16, 0.16],
            }.get(len(rows[0]), [1 / len(rows[0])] * len(rows[0]))
            table = Table(rows, colWidths=[width * r for r in ratios], repeatRows=1, hAlign='LEFT')
            table.setStyle(TableStyle([
                ('BACKGROUND', (0, 0), (-1, 0), colors.HexColor('#234C63')),
                ('ROWBACKGROUNDS', (0, 1), (-1, -1), [colors.white, colors.HexColor('#F1F5F7')]),
                ('GRID', (0, 0), (-1, -1), 0.5, colors.HexColor('#D9D9D9')),
                ('NOSPLIT', (0, 0), (-1, min(2, len(rows) - 1))),
                ('NOSPLIT', (0, -2), (-1, -1)),
                ('VALIGN', (0, 0), (-1, -1), 'MIDDLE'),
                ('LEFTPADDING', (0, 0), (-1, -1), 7), ('RIGHTPADDING', (0, 0), (-1, -1), 7),
                ('TOPPADDING', (0, 0), (-1, -1), 7), ('BOTTOMPADDING', (0, 0), (-1, -1), 7),
            ]))
            story += [table, Spacer(1, 10)]
            continue
        if line.startswith('# '):
            title = inline(line[2:], source)
            if source.name == '详细设计方案.md':
                title = 'WeKnora 质量评测基线与成本可观测<br/>详细设计方案'
            story.append(Paragraph(title, styles['title']))
            i += 1
            continue
        if line.startswith(('## ', '### ')):
            level = 0 if line.startswith('## ') else 1
            p = Paragraph(inline(line.lstrip('#').strip(), source), styles['h1' if level == 0 else 'h2'])
            p.outline_level = level
            story.append(p)
            i += 1
            continue
        if re.match(r'^(?:- |\d+\. )', line):
            line = re.sub(r'^- ', '- ', line)
            story.append(Paragraph(inline(line, source), styles['list']))
            i += 1
            continue
        paragraphs = [line]
        hard_break = lines[i].endswith('  ')
        i += 1
        while not hard_break and i < len(lines) and lines[i].strip() and not re.match(r'^(?:#|\||```|- |\d+\. )', lines[i]):
            paragraphs.append(lines[i].strip())
            hard_break = lines[i].endswith('  ')
            i += 1
        story.append(Paragraph(inline(' '.join(paragraphs), source), styles['body']))

    def page(canvas, doc):
        canvas.saveState()
        canvas.setFont(font, 8)
        canvas.setFillColor(colors.HexColor('#62737C'))
        canvas.drawString(54, A4[1] - 29, f'WeKnora 课题 3  |  徐博  |  {SUBMISSION_DATE}')
        canvas.drawRightString(A4[0] - 54, 27, str(doc.page))
        canvas.restoreState()

    output.parent.mkdir(parents=True, exist_ok=True)
    doc = ReviewPDF(str(output), pagesize=A4, leftMargin=54, rightMargin=54, topMargin=51, bottomMargin=48,
                    title=lines[0].lstrip('# '), author='徐博', subject='腾讯犀牛鸟 2026 课题 3')
    doc.build(story, onFirstPage=page, onLaterPages=page)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--font', type=Path, required=True)
    args = parser.parse_args()
    pdfmetrics.registerFont(TTFont('Chinese', str(args.font)))
    pdfmetrics.registerFontFamily('Chinese', normal='Chinese', bold='Chinese', italic='Chinese', boldItalic='Chinese')
    for name, filename in [('README.md', '3_徐博_成果与验收说明.pdf'),
                           ('详细设计方案.md', '3_徐博_WeKnora详细设计方案.pdf')]:
        build(ROOT / 'docs/submission' / name, ROOT / 'output/pdf' / filename, 'Chinese')
        print(filename)
