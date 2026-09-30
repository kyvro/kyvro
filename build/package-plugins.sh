#!/usr/bin/env bash
# package-plugins.sh — build the registry artifacts from plugins-official/.
#
# For every plugin under plugins-official/ (source of truth):
#   1. zip the payload to plugins/<id>/<id>-<version>.zip (files at archive
#      root, .DS_Store excluded)
#   2. merge its entry into plugins/list.json — versions accumulate across
#      runs (ascending SemVer), metadata/minVersions refresh from the
#      manifest; entries without a plugins-official source (already
#      published) are left untouched
#   3. rewrite plugins/lastUpdated with the same ISO 8601 stamp as
#      list.json
#
# The generated plugins/ tree is ready to commit to the kyvro/plugins
# registry repository (docs/plugin-marketplace.md).
#
# Usage: build/package-plugins.sh
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
src="$root/plugins-official"
out="$root/plugins"
stamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

command -v python3 >/dev/null 2>&1 || { echo "python3 is required" >&2; exit 1; }
[ -d "$src" ] || { echo "missing $src" >&2; exit 1; }

python3 - "$src" "$out" "$stamp" <<'PY'
import json
import re
import sys
import zipfile
from pathlib import Path

src, out, stamp = Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3]
junk = {".DS_Store"}


def semver_key(v):
    nums = re.findall(r"\d+", v)
    return tuple(int(n) for n in nums) if nums else (0,)


list_path = out / "list.json"
if list_path.exists():
    registry = json.loads(list_path.read_text(encoding="utf-8"))
else:
    registry = {"version": 1, "lastUpdated": stamp, "plugins": []}
if registry.get("version") != 1:
    sys.exit(f"{list_path}: unsupported schema version {registry.get('version')}")
entries = {p["id"]: p for p in registry.get("plugins", [])}

manifests = sorted(src.glob("*/plugin.json"))
if not manifests:
    sys.exit(f"no plugin.json found under {src}")

for mpath in manifests:
    m = json.loads(mpath.read_text(encoding="utf-8"))
    pid, ver = m["id"], m["version"]
    pdir = mpath.parent

    dest_dir = out / pid
    dest_dir.mkdir(parents=True, exist_ok=True)
    zpath = dest_dir / f"{pid}-{ver}.zip"
    files = sorted(f for f in pdir.rglob("*") if f.is_file() and f.name not in junk)
    with zipfile.ZipFile(zpath, "w", zipfile.ZIP_DEFLATED) as z:
        for f in files:
            z.write(f, f.relative_to(pdir).as_posix())

    e = entries.setdefault(pid, {"id": pid})
    old_versions = e.get("versions", [])
    minmap = dict(zip(old_versions, e.get("minVersions", [])))
    minmap[ver] = m.get("minHostVersion", "0.1.0")
    ordered = sorted(set(old_versions) | {ver}, key=semver_key)
    e.update({
        "name": m.get("name", pid),
        "description": m.get("description", ""),
        "author": m.get("author") or {},
        "permissions": m.get("permissions", []),
        "platforms": m.get("platforms", []),
        "versions": ordered,
        "minVersions": [minmap.get(v, minmap[ver]) for v in ordered],
    })
    print(f"packed {pid} {ver} -> {zpath.relative_to(out.parent)} ({len(files)} files)")

registry["plugins"] = [entries[k] for k in sorted(entries)]
registry["lastUpdated"] = stamp
list_path.write_text(json.dumps(registry, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
(out / "lastUpdated").write_text(stamp + "\n", encoding="utf-8")
print(f"list.json: {len(registry['plugins'])} plugin(s); lastUpdated {stamp}")
PY
