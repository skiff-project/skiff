#!/usr/bin/env python3
"""Writes THIRD_PARTY_NOTICES.txt: the software Skiff includes, with the
licence and notices of each (what MIT, BSD and Apache-2.0 ask for when the
program is distributed).

  build/notices.py           rewrite the file
  build/notices.py --check   fail if the file is not up to date (./build.sh test)

It lists the Go modules compiled into Skiff on every system (go list) and
the JavaScript packages of the interface (pnpm licenses), so the module
cache and frontend/node_modules must be there.
"""
import difflib
import json
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "THIRD_PARTY_NOTICES.txt")

# the builds of Skiff: every system, with and without the native window
BUILDS = [
    ("linux", "", "0"),
    ("linux", "desktop,production,webkit2_41", "1"),
    ("windows", "desktop,production", "0"),
    ("darwin", "", "0"),
    ("darwin", "desktop,production", "1"),
    ("freebsd", "", "0"),
]


def run(cmd, cwd=ROOT, env=None):
    return subprocess.run(cmd, cwd=cwd, env=env, check=True, capture_output=True, text=True).stdout


def licence_file(d):
    names = sorted(os.listdir(d)) if os.path.isdir(d) else []
    for n in names:
        if re.match(r"(?i)^(licen[cs]e|copying)(\.(md|txt))?$", n):
            return os.path.join(d, n)
    for n in names:
        if re.match(r"(?i)^licen[cs]e", n):
            return os.path.join(d, n)
    return None


def notice_file(d):
    for n in sorted(os.listdir(d)) if os.path.isdir(d) else []:
        if re.match(r"(?i)^notice(\.(md|txt))?$", n):
            return os.path.join(d, n)
    return None


def read(p):
    with open(p, encoding="utf-8", errors="replace") as f:
        return f.read().replace("\r\n", "\n").strip("\n")


def is_apache(text):
    return "Apache License" in text and "Version 2.0" in text and "TERMS AND CONDITIONS FOR USE" in text


def spdx(text):
    """The licences of a licence text (a file may hold several)."""
    t = " ".join(text.split())
    if "Unlicense OR MIT" in t:
        return "Unlicense OR MIT"
    found = []
    if "Permission is hereby granted, free of charge" in t:
        found.append("MIT")
    if "Redistribution and use in source and binary forms" in t:
        found.append("BSD-3-Clause" if "Neither the name" in t or "names of its contributors" in t else "BSD-2-Clause")
    if "Permission to use, copy, modify, and/or distribute" in t or "Permission to use, copy, modify, and distribute" in t:
        found.append("ISC")
    if is_apache(text) or "Apache License, Version 2.0" in t or "covered by two different licenses: MIT and Apache" in t:
        found.append("Apache-2.0")
    if "This is free and unencumbered software" in t:
        found.append("Unlicense")
    return " AND ".join(found) or "see the text"


def go_modules():
    mods = {}
    for goos, tags, cgo in BUILDS:
        env = dict(os.environ, GOOS=goos, CGO_ENABLED=cgo, GOFLAGS="")
        out = run(["go", "list", "-deps", "-tags", tags, "-f", "{{with .Module}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}", "."], env=env)
        for line in out.splitlines():
            if not line.strip():
                continue
            path, version, d = line.split("\t")
            if path != "skiff":
                mods[path] = (version, d)
    return mods


def js_packages():
    out = run(["pnpm", "licenses", "list", "--prod", "--json"], cwd=os.path.join(ROOT, "frontend"))
    pkgs = {}
    for lic, items in json.loads(out).items():
        for it in items:
            for v, p in zip(it.get("versions", []), it.get("paths", [])):
                pkgs[(it["name"], v)] = (lic, p)
    return pkgs


def entry(title, licence, text, notice):
    lines = ["=" * 78, title, "Licence: " + licence, "-" * 78]
    if text is None:
        lines.append("(no licence file in the package)")
    elif licence == "Apache-2.0" and is_apache(text):
        # the same licence as Skiff, alone: one copy (LICENSE) is enough
        lines.append("Apache License, Version 2.0: the same text as the LICENSE file of Skiff.")
    else:
        lines.append(text)
    if notice:
        lines += ["", "NOTICE:", notice]
    return "\n".join(lines)


def build():
    goroot = run(["go", "env", "GOROOT"]).strip()
    # Go 1.27, not go1.27.3: the file must not depend on the patch release of
    # the machine that makes it (the CI may have a newer one)
    gover = run(["go", "env", "GOVERSION"]).strip()
    m = re.match(r"go(\d+\.\d+)", gover)
    gover = "Go " + (m.group(1) if m else gover)
    parts = []
    toc = []
    text = read(os.path.join(goroot, "LICENSE"))
    toc.append(f"  {gover} (standard library and runtime) — {spdx(text)}")
    parts.append(entry(f"{gover} (standard library and runtime)", spdx(text), text, None))
    mods = go_modules()
    for path in sorted(mods, key=str.lower):
        version, d = mods[path]
        lf, nf = licence_file(d), notice_file(d)
        text = read(lf) if lf else None
        lic = spdx(text) if text else "unknown"
        toc.append(f"  {path} {version} — {lic}")
        parts.append(entry(f"{path} {version}", lic, text, read(nf) if nf else None))
    jtoc = []
    pkgs = js_packages()
    for name, version in sorted(pkgs, key=lambda k: (k[0].lower(), k[1])):
        lic, d = pkgs[(name, version)]
        lf, nf = licence_file(d), notice_file(d)
        text = read(lf) if lf else None
        jtoc.append(f"  {name} {version} — {lic}")
        parts.append(entry(f"{name} {version} (npm)", lic, text, read(nf) if nf else None))
    head = "\n".join([
        "Third-party software in Skiff",
        "",
        "Skiff is licensed under the Apache License, Version 2.0 (see LICENSE and",
        "NOTICE). It includes the following software, each under its own licence,",
        "whose texts and notices follow. This file is made by build/notices.py.",
        "",
        "Go (compiled into the program):",
        *toc,
        "",
        "JavaScript (the interface, embedded in the program):",
        *jtoc,
        "",
    ])
    return head + "\n" + "\n\n".join(parts) + "\n"


def main():
    text = build()
    if "--check" in sys.argv:
        cur = read(OUT) + "\n" if os.path.exists(OUT) else ""
        if cur != text:
            diff = difflib.unified_diff(cur.splitlines(), text.splitlines(), "THIRD_PARTY_NOTICES.txt", "expected", lineterm="", n=1)
            sys.stderr.write("\n".join(list(diff)[:40]) + "\n")
            sys.exit("THIRD_PARTY_NOTICES.txt is not up to date: run build/notices.py")
        print("THIRD_PARTY_NOTICES.txt up to date")
        return
    with open(OUT, "w", encoding="utf-8") as f:
        f.write(text)
    print("wrote THIRD_PARTY_NOTICES.txt")


if __name__ == "__main__":
    main()
