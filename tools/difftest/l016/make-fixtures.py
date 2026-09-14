#!/usr/bin/env python3
"""L016-1 pypi simple fixtures — real sdist/wheel + invalid-wheel 3-state set.

Builds into <outdir> (default /tmp/l016/fixtures). All dists are real
(zip archives with dist-info METADATA / tar.gz with PKG-INFO) so that
twine 7 parses them client-side like a genuine publisher.

States:
  hello16-1.0.0.tar.gz               good sdist  (Requires-Python: >=3.8)
  hello16-1.0.0-py3-none-any.whl     good wheel  (METADATA Requires-Python: >=3.8)
  hello16-1.0.1.tar.gz               yanked-arm sdist (uploaded with yanked form field via curl)
  badname/notl016-1.0.0-py3-none-any.whl   valid zip, METADATA Name=notl016  (filename-vs-form mismatch)
  badmeta/nol016-1.0.0-py3-none-any.whl      valid zip, NO METADATA file (unique name: no path collision)
  badver/hello16-abc-py3-none-any.whl       valid zip, METADATA Version=abc (non PEP 440)
"""
import io, os, tarfile, zipfile, sys

OUT = sys.argv[1] if len(sys.argv) > 1 else "/tmp/l016/fixtures"

META_COMMON = "Metadata-Version: 2.1\nName: hello16\nVersion: 1.0.0\nSummary: l016 pypi differential probe\nRequires-Python: >=3.8\n"
WHEEL_FILE = "Wheel-Version: 1.0\nGenerator: l016-fixture\nRoot-Is-Purelib: true\nTag: py3-none-any\n"
INIT = '"""l016 probe package"""\n__version__ = "1.0.0"\n'


def wheel_bytes(filename: str, metadata: str, dist_info: str = None) -> bytes:
    name, rest = filename.split("-", 1)
    parts = filename[:-4].split("-")  # {name}-{version}-{pythag}-{abi}-{plat} (names here are single-segment)
    di = dist_info or f"{parts[0]}-{parts[1]}.dist-info"  # PEP 427: {name}-{version}.dist-info
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr(f"{name}/__init__.py", INIT)
        if metadata is not None:
            z.writestr(f"{di}/METADATA", metadata)
        z.writestr(f"{di}/WHEEL", WHEEL_FILE)
        z.writestr(f"{di}/RECORD", f"{di}/METADATA,\n{di}/WHEEL,\n{di}/RECORD,\n{name}/__init__.py,\n")
    return buf.getvalue()


def sdist_bytes(pkg_dir: str, metadata: str) -> bytes:
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as t:
        def add(path, data):
            data = data.encode()
            ti = tarfile.TarInfo(path)
            ti.size = len(data)
            ti.mtime = 0
            t.addfile(ti, io.BytesIO(data))
        add(f"{pkg_dir}/PKG-INFO", metadata)
        add(f"{pkg_dir}/hello16/__init__.py", INIT)
        add(f"{pkg_dir}/setup.py", "from setuptools import setup\nsetup(name='hello16', version=pkg_dir.split('-')[-1], py_modules=[])\n")
    return buf.getvalue()


def main():
    for d in ("", "badname", "badmeta", "badver"):
        os.makedirs(os.path.join(OUT, d), exist_ok=True)
    w = open(os.path.join(OUT, "hello16-1.0.0-py3-none-any.whl"), "wb")
    w.write(wheel_bytes("hello16-1.0.0-py3-none-any.whl", META_COMMON)); w.close()
    y = open(os.path.join(OUT, "hello16-1.0.1.tar.gz"), "wb")
    y.write(sdist_bytes("hello16-1.0.1", META_COMMON.replace("Version: 1.0.0", "Version: 1.0.1"))); y.close()
    s = open(os.path.join(OUT, "hello16-1.0.0.tar.gz"), "wb")
    s.write(sdist_bytes("hello16-1.0.0", META_COMMON)); s.close()
    open(os.path.join(OUT, "badname", "notl016-1.0.0-py3-none-any.whl"), "wb").write(
        wheel_bytes("notl016-1.0.0-py3-none-any.whl",
                    "Metadata-Version: 2.1\nName: notl016\nVersion: 1.0.0\nSummary: l016 badname arm\n"))
    open(os.path.join(OUT, "badmeta", "nol016-1.0.0-py3-none-any.whl"), "wb").write(
        wheel_bytes("nol016-1.0.0-py3-none-any.whl",
                    None, dist_info="nol016-1.0.0.dist-info"))  # no METADATA at all
    open(os.path.join(OUT, "badver", "hello16-abc-py3-none-any.whl"), "wb").write(
        wheel_bytes("hello16-abc-py3-none-any.whl",
                    "Metadata-Version: 2.1\nName: hello16\nVersion: abc\nSummary: l016 badver arm\n"))
    print("fixtures in", OUT)
    for root, _, files in os.walk(OUT):
        for f in sorted(files):
            p = os.path.join(root, f)
            print(f"  {os.path.getsize(p):>7}B  {os.path.relpath(p, OUT)}")


if __name__ == "__main__":
    main()
