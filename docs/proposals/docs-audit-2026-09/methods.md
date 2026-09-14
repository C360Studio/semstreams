# Documentation audit method and corpus

The baseline is `ea22e6a4e75d12bf7f6050c6d8121de1d089b191`. Only the audit's new Markdown records differ from it.
The census enumerates every tracked Markdown file at that commit, excluding the audit itself. It does not equate
enumeration with semantic review. Package `doc.go` files are separately counted: 85 files, 13,235 lines
(including root `doc.go`); selected public examples were checked, while all other exported Go comments remain
outside exhaustive coverage.

## Corpus

| Location/function | Files | Lines |
| --- | ---: | ---: |
| Agent instructions and adapters | 54 | 7,013 |
| Repository/package/example/test docs | 95 | 26,134 |
| Docs entry points | 2 | 412 |
| ADRs and ADR index | 85 | 24,728 |
| docs/advanced | 11 | 5,021 |
| docs/basics | 10 | 4,706 |
| docs/concepts | 30 | 9,177 |
| docs/contributing | 7 | 2,564 |
| docs/operations | 93 | 18,222 |
| Proposal and audit records | 152 | 57,778 |
| OpenSpec archives | 592 | 88,955 |
| Current capability specs | 53 | 16,109 |

Total: **1,184 Markdown files / 260,819 lines**. The 93 operations pages include 43 `migration-*` files
(8,024 lines), 48 other root pages (9,894 lines), and two evidence pages (304 lines). Filename groups are not
authority classifications: migration and proposal records require their own status/links to be interpreted.

## Checks and limits

The mechanical pass found 1,213 relative Markdown link occurrences. Of these, 140 resolve to absent repository
targets: 87 distinct targets across 59 source pages. Three fragments are candidates for stale anchors. These are
triage counts, not 140 independently verified current-user defects. Historical links and placeholder examples can
legitimately refer to material outside today's tree. Representative current-guide targets were checked manually.

The scanner ignores fenced and inline code when extracting links, handles ordinary inline and reference links,
resolves leading-slash paths at repository root, and checks tracked files/directories. It approximates GitHub
heading slugs. It does not check remote HTTP availability, HTML link attributes, all Markdown extensions, Go imports,
or compile embedded snippets. Exact paragraph matches are candidates only; archived spec repetition and generated
platform workflows account for much of that overlap. No removal estimate is derived from that candidate count.

Semantic evidence is excerpt-level, listed in searches.md. Entire guides were not certified. The reviewer checks
the strongest counterexamples to the selected conclusions; the inventory verifier checks pin drift, not completeness.

## Reproduce the mechanical pass

Run from a checkout containing the recorded baseline with its document files unchanged. Python 3's standard library
is sufficient. Extract the following Python fence to a temporary file and run it from the repository root. It writes
the full census to `/private/tmp/semstreams-docs-audit-census.json` and prints aggregate counts. The file is an audit
helper, not a new repository script or CI gate. The source is preserved here so the result is reproducible after the
authoring session ends.

```python
from pathlib import Path
import collections
import hashlib
import json
import posixpath
import re
import subprocess
import urllib.parse

BASE = 'ea22e6a4e75d12bf7f6050c6d8121de1d089b191'
root = Path.cwd()
tracked = subprocess.check_output(['git', 'ls-tree', '-r', '--name-only', BASE], text=True).splitlines()
paths = set(tracked)
directories = {posixpath.dirname(p) for p in tracked}
for directory in list(directories):
    while directory:
        directory = posixpath.dirname(directory)
        directories.add(directory)
md = sorted(p for p in tracked if p.endswith('.md'))
texts = {p: (root / p).read_text() for p in md}
# The audit worktree changes only new audit records. Confirm all inventoried bytes remain at BASE.
changed = subprocess.check_output(['git', 'diff', '--name-only', BASE], text=True).splitlines()
assert not set(changed) & set(md), 'baseline documentation changed'


def category(path):
    if path.startswith('openspec/changes/archive/'):
        return 'OpenSpec archives'
    if path.startswith('openspec/specs/'):
        return 'Current capability specs'
    if path.startswith('docs/proposals/'):
        return 'Proposal and audit records'
    if path.startswith('docs/adr/'):
        return 'ADRs and ADR index'
    if path.startswith('docs/'):
        return '/'.join(path.split('/')[:2]) if path.count('/') > 1 else 'Docs entry points'
    if path.startswith(('.agents/', '.claude/', '.codex/')) or path in ['CLAUDE.md', 'AGENTS.md']:
        return 'Agent instructions and adapters'
    return 'Repository/package/example/test docs'


def prose(text, strip_inline=True):
    lines = []
    fence = None
    for line in text.splitlines(keepends=True):
        marker = re.match(r'^\s{0,3}(`{3,}|~{3,})', line)
        if marker:
            value = marker.group(1)
            if fence is None:
                fence = value
            elif value[0] == fence[0] and len(value) >= len(fence):
                fence = None
            lines.append('\n')
        elif fence:
            lines.append('\n')
        else:
            lines.append(re.sub(r'(`+)(.*?)\1', lambda m: ' ' * len(m.group(0)), line)
                         if strip_inline else line)
    return ''.join(lines)


def slug(value):
    value = re.sub(r'\[([^\]]+)\]\([^)]*\)', r'\1', value)
    value = re.sub(r'<[^>]+>', '', value)
    value = re.sub(r'[`*_~]', '', value).lower()
    return re.sub(r'[^\w\- ]', '', value).replace(' ', '-')


anchors = {}
for path, text in texts.items():
    values = set(re.findall(r'(?:id|name)=["\']([^"\']+)["\']', text))
    seen = collections.Counter()
    for heading in re.findall(r'^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$', text, re.M):
        name = slug(heading)
        values.add(name + (f'-{seen[name]}' if seen[name] else ''))
        seen[name] += 1
    anchors[path] = values


links = []
missing = []
anchor_candidates = []
unresolved = []
paragraphs = collections.defaultdict(list)
corpus = []
for path, original in texts.items():
    text = prose(original)
    defs = {re.sub(r'\s+', ' ', k).lower(): v for k, v in
            re.findall(r'^\s{0,3}\[([^\]]+)\]:\s*<?([^\s>]+)>?', text, re.M)}
    occupied = []
    occurrences = []
    # Ordinary Markdown inline links/images; nested parentheses are tracked.
    for m in re.finditer(r'!?\[([^\]\n]+)\]\(', text):
        start = m.end()
        depth = 1
        cursor = start
        while cursor < len(text) and depth:
            if text[cursor] == '(' and text[cursor - 1] != '\\':
                depth += 1
            elif text[cursor] == ')' and text[cursor - 1] != '\\':
                depth -= 1
            cursor += 1
        if depth == 0:
            inside = text[start:cursor - 1].strip()
            target = inside[1:inside.find('>')] if inside.startswith('<') else inside.split()[0] if inside else ''
            occurrences.append((m.start(), target))
            occupied.append((m.start(), cursor))
    for m in re.finditer(r'!?\[([^\]\n]+)\]\[([^\]\n]*)\]', text):
        key = re.sub(r'\s+', ' ', m.group(2) or m.group(1)).lower()
        if key in defs:
            occurrences.append((m.start(), defs[key]))
        else:
            unresolved.append({'source': path, 'line': text.count('\n', 0, m.start()) + 1, 'label': key})
        occupied.append((m.start(), m.end()))
    for m in re.finditer(r'\[([^\]\n]+)\](?![:(\[])', text):
        if any(a <= m.start() < b for a, b in occupied):
            continue
        key = re.sub(r'\s+', ' ', m.group(1)).lower()
        if key in defs:
            occurrences.append((m.start(), defs[key]))
    for offset, target in occurrences:
        if not target or re.match(r'^[a-zA-Z][a-zA-Z0-9+.-]*:', target) or target.startswith('//'):
            continue
        file_part, _, fragment = target.partition('#')
        file_part = urllib.parse.unquote(file_part).split('?')[0]
        destination = (posixpath.normpath(file_part.lstrip('/')) if file_part.startswith('/') else
                       posixpath.normpath(posixpath.join(posixpath.dirname(path), file_part))) if file_part else path
        link = {'source': path, 'line': text.count('\n', 0, offset) + 1,
                'target': target, 'resolved': destination, 'category': category(path)}
        links.append(link)
        if destination not in paths and destination not in directories:
            missing.append(link)
        elif fragment and destination in anchors and urllib.parse.unquote(fragment) not in anchors[destination]:
            anchor_candidates.append(link)
    paragraph_text = prose(original, strip_inline=False)
    for match in re.finditer(r'(?:^|\n\s*\n)([^\n].*?)(?=\n\s*\n|\Z)', paragraph_text, re.S):
        paragraph = match.group(1).strip()
        if any(line.startswith(('#', '|', '[', '- ', '* ', '>')) for line in paragraph.splitlines()):
            continue
        normalized = re.sub(r'\s+', ' ', paragraph)
        if len(normalized.split()) >= 25:
            paragraphs[normalized].append({'path': path, 'line': paragraph_text.count('\n', 0, match.start(1)) + 1})
    corpus.append({'path': path, 'category': category(path), 'lines': len(original.splitlines()),
                   'words': len(original.split()), 'sha256': hashlib.sha256(original.encode()).hexdigest()})

groups = collections.defaultdict(lambda: {'files': 0, 'lines': 0, 'words': 0})
for item in corpus:
    for name, amount in [('files', 1), ('lines', item['lines']), ('words', item['words'])]:
        groups[item['category']][name] += amount
duplicates = [{'words': len(text.split()), 'text': text, 'occurrences': places}
              for text, places in paragraphs.items() if len({v['path'] for v in places}) > 1]
duplicates.sort(key=lambda item: -item['words'] * (len(item['occurrences']) - 1))
data = {'base': BASE, 'corpus': corpus, 'groups': groups, 'links': links,
        'missing_targets': missing, 'anchor_candidates': anchor_candidates,
        'unresolved_reference_candidates': unresolved, 'duplicate_paragraphs': duplicates}
Path('/private/tmp/semstreams-docs-audit-census.json').write_text(json.dumps(data, indent=2))
print(json.dumps({'base': BASE, 'files': len(corpus), 'groups': groups,
                  'relative_link_occurrences': len(links), 'missing_target_occurrences': len(missing),
                  'missing_target_sources': len({v['source'] for v in missing}),
                  'anchor_candidates': len(anchor_candidates), 'undefined_reference_candidates': len(unresolved),
                  'exact_duplicate_paragraph_groups': len(duplicates)}, indent=2))
```
