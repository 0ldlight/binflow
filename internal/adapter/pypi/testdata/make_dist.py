"""Build minimal but fully valid wheels and sdists for the T-70 client tests.

Usage: make_dist.py <outdir> <spec>...
    spec = wheel:<name>:<version>[:<requires>,...]

The wheel carries a correct RECORD (pip verifies install-time hashes), a
METADATA with Metadata-Version 2.1 and optional Requires-Dist lines, and a
WHEEL marker with the py3-none-any tag. The sdist carries PKG-INFO plus a
setup.py (setuptools backend) so `pip install --no-binary` can build it,
including install_requires pulled from the spec.
"""

import base64
import csv
import hashlib
import io
import os
import sys
import tarfile
import zipfile


def urlsafe_b64_nopad(digest: bytes) -> str:
    return base64.urlsafe_b64encode(digest).rstrip(b"=").decode("ascii")


def record_hash(data: bytes) -> str:
    return "sha256=" + urlsafe_b64_nopad(hashlib.sha256(data).digest())


def metadata_text(name: str, version: str, requires) -> str:
    lines = [
        "Metadata-Version: 2.1",
        f"Name: {name}",
        f"Version: {version}",
        "Summary: T-70 client-test fixture",
    ]
    for req in requires or []:
        lines.append(f"Requires-Dist: {req}")
    lines.append("")
    return "\n".join(lines)


def build_wheel(outdir: str, name: str, version: str, requires) -> str:
    dist = f"{name}-{version}.dist-info"
    files = {
        f"{name}/__init__.py": f"__version__ = {version!r}\nVALUE = {version!r}\n",
        f"{dist}/METADATA": metadata_text(name, version, requires),
        f"{dist}/WHEEL": (
            "Wheel-Version: 1.0\nGenerator: t70-fixture\nRoot-Is-Purelib: true\nTag: py3-none-any\n"
        ),
    }
    record = []
    for path, data in sorted(files.items()):
        record.append((path, record_hash(data.encode()), str(len(data))))
    record.append((f"{dist}/RECORD", "", ""))
    files[f"{dist}/RECORD"] = "\n".join(",".join(row) for row in record) + "\n"

    out = os.path.join(outdir, f"{name}-{version}-py3-none-any.whl")
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as zf:
        for path, data in sorted(files.items()):
            zf.writestr(path, data)
    return out


def setup_py(name: str, version: str, requires) -> str:
    reqs = ", ".join(repr(r) for r in (requires or []))
    return (
        "from setuptools import setup\n"
        f"setup(name={name!r}, version={version!r}, py_modules=[{name!r}], "
        f"install_requires=[{reqs}])\n"
    )


def build_sdist(outdir: str, name: str, version: str, requires) -> str:
    root = f"{name}-{version}"
    members = {
        f"{root}/PKG-INFO": metadata_text(name, version, requires),
        f"{root}/setup.py": setup_py(name, version, requires),
        f"{root}/pyproject.toml": (
            "[build-system]\nrequires = [\"setuptools\"]\nbuild-backend = \"setuptools.build_meta\"\n"
        ),
        f"{root}/{name}.py": f"__version__ = {version!r}\nVALUE = {version!r}\n",
    }
    out = os.path.join(outdir, f"{root}.tar.gz")
    with tarfile.open(out, "w:gz") as tf:
        for path, data in sorted(members.items()):
            info = tarfile.TarInfo(path)
            info.size = len(data)
            tf.addfile(info, io.BytesIO(data.encode()))
    return out


def main() -> int:
    outdir, specs = sys.argv[1], sys.argv[2:]
    os.makedirs(outdir, exist_ok=True)
    for spec in specs:
        parts = spec.split(":")
        kind, name = parts[0], parts[1]
        version = parts[2] if len(parts) > 2 else "0.1.0"
        requires = parts[3].split(",") if len(parts) > 3 and parts[3] else []
        if kind == "wheel":
            print(build_wheel(outdir, name, version, requires))
        elif kind == "sdist":
            print(build_sdist(outdir, name, version, requires))
        else:
            raise SystemExit(f"unknown kind {kind}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
