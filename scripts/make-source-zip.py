#!/usr/bin/env python3
import argparse
import os
import pathlib
import stat
import zipfile

parser = argparse.ArgumentParser()
parser.add_argument("--source", default=".")
parser.add_argument("--output", required=True)
parser.add_argument("--prefix", required=True)
args = parser.parse_args()
root = pathlib.Path(args.source).resolve()
output = pathlib.Path(args.output).resolve()
excludes = {".git", "dist", "bin", "build", ".worldbisect"}

with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
    for path in sorted(root.rglob("*")):
        relative = path.relative_to(root)
        if any(part in excludes for part in relative.parts) or path == output:
            continue
        if path.is_dir():
            continue
        name = f"{args.prefix}/{relative.as_posix()}"
        info = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
        mode = path.stat().st_mode
        info.external_attr = (stat.S_IFREG | stat.S_IMODE(mode)) << 16
        info.compress_type = zipfile.ZIP_DEFLATED
        archive.writestr(info, path.read_bytes())
