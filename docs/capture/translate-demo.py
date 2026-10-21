#!/usr/bin/env python3
"""Translates the demo data of the screenshots (demo-setup.sh) in place:
the names of the files and folders and the texts in the files, phrase by
phrase, with a dictionary such as demo-en.json ({"french": "english"}).
The dates of the files stay as they were.

  translate-demo.py <demo folder> <dictionary.json>

The capture script reads the same dictionary, so that the names it clicks
are the ones made here.
"""
import json
import os
import re
import sys


def phrases(table):
    # the longest first ("Site vitrine (SFTP)" before "Site vitrine"), whole
    # words only ("Nom" is not in "Nombre")
    keys = sorted((k for k, v in table.items() if k != v), key=len, reverse=True)
    if not keys:
        return lambda s: s
    rx = re.compile("|".join(r"(?<!\w)" + re.escape(k) + r"(?!\w)" for k in keys))
    return lambda s: rx.sub(lambda m: table[m.group(0)], s)


def main():
    root, dict_path = sys.argv[1], sys.argv[2]
    with open(dict_path, encoding="utf-8") as f:
        table = json.load(f)
    tr = phrases(table)
    # the contents first, then the names, from the deepest
    for d, dirs, files in os.walk(root):
        for n in files:
            p = os.path.join(d, n)
            if os.path.islink(p) or os.path.getsize(p) > 2_000_000:
                continue
            with open(p, "rb") as f:
                raw = f.read()
            try:
                text = raw.decode("utf-8")
            except UnicodeDecodeError:
                continue  # images, archives
            new = tr(text)
            if new != text:
                st = os.stat(p)
                with open(p, "w", encoding="utf-8") as f:
                    f.write(new)
                os.utime(p, (st.st_atime, st.st_mtime))
    for d, dirs, files in os.walk(root, topdown=False):
        for n in files + dirs:
            new = tr(n)
            if new != n:
                p, q = os.path.join(d, n), os.path.join(d, new)
                st = os.lstat(p)
                os.rename(p, q)
                if not os.path.islink(q):
                    os.utime(q, (st.st_atime, st.st_mtime))
    print("demo translated with", os.path.basename(dict_path))


if __name__ == "__main__":
    main()
