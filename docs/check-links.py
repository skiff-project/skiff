#!/usr/bin/env python3
"""Checks the links of the documentation, in both languages.

Every relative link and image of README.md, README.en.md, docs/*.md and
docs/en/*.md must lead to a file that exists and, with a #fragment, to a
heading of that file (anchors as GitHub makes them). Each chapter
docs/NN-*.md must have its English counterpart docs/en/NN-*.md, the same
number, and the other way round, each linking to the other.

    python3 docs/check-links.py
"""
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DOCS = os.path.join(ROOT, 'docs')
EN = os.path.join(DOCS, 'en')

LINK = re.compile(r'!?\[(?:[^\]\\]|\\.)*\]\(([^)\s]+)(?:\s+"[^"]*")?\)')
FENCE = re.compile(r'^(```|~~~)')


def slug(text):
    """The anchor GitHub gives a heading."""
    text = re.sub(r'\[([^\]]*)\]\([^)]*\)', r'\1', text)  # links: their text
    text = re.sub(r'[`*]', '', text).strip().lower()
    text = ''.join(c for c in text if c.isalnum() or c in ' -_')
    return text.replace(' ', '-')


def prose(path, keep_code=False):
    """The lines of a Markdown file outside code blocks, without inline code
    (kept for the headings, whose anchor holds it)."""
    out, code = [], False
    with open(path, encoding='utf-8') as f:
        for n, line in enumerate(f, 1):
            if FENCE.match(line.strip()):
                code = not code
                continue
            if not code:
                out.append((n, line if keep_code else re.sub(r'`[^`]*`', '', line)))
    return out


anchors_cache = {}


def anchors(path):
    if path not in anchors_cache:
        seen, found = {}, set()
        for _, line in prose(path, keep_code=True):
            m = re.match(r'^#{1,6}\s+(.*?)\s*#*\s*$', line)
            if m:
                s = slug(m.group(1))
                k = seen.get(s, 0)
                seen[s] = k + 1
                found.add(s if k == 0 else f'{s}-{k}')
        anchors_cache[path] = found
    return anchors_cache[path]


def main():
    files = [os.path.join(ROOT, 'README.md'), os.path.join(ROOT, 'README.en.md')]
    for d in (DOCS, EN):
        files += sorted(os.path.join(d, f) for f in os.listdir(d) if f.endswith('.md'))
    bad = []
    for path in files:
        rel = os.path.relpath(path, ROOT)
        for n, line in prose(path):
            for m in LINK.finditer(line):
                target = m.group(1)
                if re.match(r'^[a-z]+:', target):
                    continue  # https:, mailto:
                file, _, frag = target.partition('#')
                dest = os.path.normpath(os.path.join(os.path.dirname(path), file)) if file else path
                if not os.path.exists(dest):
                    bad.append(f'{rel}:{n}: {target}: no such file')
                elif frag and dest.endswith('.md') and frag not in anchors(dest):
                    bad.append(f'{rel}:{n}: {target}: no such heading')
    # each chapter in both languages, linking to the other
    def chapters(d):
        return {f[:2]: f for f in os.listdir(d) if re.match(r'^\d\d-.*\.md$', f)}
    fr, en = chapters(DOCS), chapters(EN)
    for num in sorted(set(fr) | set(en)):
        if num not in en:
            bad.append(f'docs/{fr[num]}: no English chapter docs/en/{num}-….md')
        elif num not in fr:
            bad.append(f'docs/en/{en[num]}: no French chapter docs/{num}-….md')
        else:
            for here, there in ((f'docs/{fr[num]}', f'en/{en[num]}'), (f'docs/en/{en[num]}', f'../{fr[num]}')):
                with open(os.path.join(ROOT, here), encoding='utf-8') as f:
                    if f'({there})' not in f.read():
                        bad.append(f'{here}: no link to its other language ({there})')
    for b in bad:
        print(b)
    print(f'{len(files)} files, {len(bad)} broken link(s)')
    return 1 if bad else 0


if __name__ == '__main__':
    sys.exit(main())
