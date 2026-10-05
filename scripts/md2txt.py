#!/usr/bin/env python3
"""把使用说明书 Markdown 转为随 exe 分发的纯文本版本（记事本可直接打开）。
用法：python3 scripts/md2txt.py <输入.md> <输出.txt>
"""
import re
import sys


def inline(line):
    """去除行内 Markdown 格式（粗体/斜体/代码/链接）"""
    line = re.sub(r'\*\*(.+?)\*\*', r'\1', line)
    line = re.sub(r'\*(.+?)\*', r'\1', line)
    line = re.sub(r'`(.+?)`', r'\1', line)
    line = re.sub(r'\[(.+?)\]\([^)]+\)', r'\1', line)
    return line


def convert(text):
    out = []
    in_code = False
    for raw in text.splitlines():
        line = raw.rstrip()
        if line.startswith('```'):
            in_code = not in_code
            continue
        if in_code:
            out.append('    ' + line)
            continue
        if re.match(r'^-{3,}$', line):
            continue  # 水平分隔线
        # 表格：分隔行跳过，数据行用 " | " 连接
        if line.startswith('|'):
            cells = [c.strip() for c in line.strip('|').split('|')]
            if cells and all(set(c) <= set('-: ') for c in cells):
                continue
            out.append('  ' + ' | '.join(inline(c) for c in cells))
            continue
        # 标题：去掉 # 号，一级标题加下划线
        m = re.match(r'^(#{1,6})\s*(.*)$', line)
        if m:
            level, title = len(m.group(1)), m.group(2).strip()
            out.append(inline(title))
            if level <= 2:
                out.append('=' * len(title))
            continue
        # 引用、行内格式
        line = re.sub(r'^>\s?', '　', line)
        out.append(inline(line))
    # 压缩连续空行
    text = '\n'.join(out)
    text = re.sub(r'\n{3,}', '\n\n', text)
    return text.strip() + '\n'


if __name__ == '__main__':
    src, dst = sys.argv[1], sys.argv[2]
    with open(src, encoding='utf-8') as f:
        data = f.read()
    with open(dst, 'w', encoding='utf-8') as f:
        f.write(convert(data))
    print(f'已生成 {dst}')
