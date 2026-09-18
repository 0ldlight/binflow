#!/usr/bin/env python3
"""Build the Artifactory interaction inventory from catalog-v2 DOM summaries."""
import glob
import json
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
MANIFEST = ROOT / "tools/penpot-sync/capture/crawl-manifest.json"
CATALOG_GLOB = str(ROOT / "docs/reverse/frontend/parity-capture/catalog-v2-20260918/*/screens-catalog.json")
OUTPUT = ROOT / "docs/reverse/frontend/interaction-inventory.yaml"
SHELL_BUTTONS = {"Platform", "Administration", "A"}
WELCOME_BUTTONS = {"Get Started"}


def unique(values):
    return list(dict.fromkeys(v.strip() for v in values if v and v.strip()))


def yq(value):
    value = str(value)
    if value == "":
        return '""'
    safe = all(ch.isalnum() or ch in "._-/@:+ ()[]{}<>,;=&?%$#" for ch in value)
    if not safe or value.startswith(" ") or value.endswith(" "):
        return json.dumps(value, ensure_ascii=False)
    return value


def yseq(name, values, indent=2):
    if not values:
        return f"{' ' * indent}{name}: []"
    lines = [f"{' ' * indent}{name}:"]
    for value in values:
        lines.append(f"{' ' * (indent + 2)}- {yq(value)}")
    return "\n".join(lines)


manifest = json.loads(MANIFEST.read_text())
manifest_by_slug = {row["slug"]: row for row in manifest["screens"]}
rows = []
for filename in glob.glob(CATALOG_GLOB):
    rows.extend(json.loads(Path(filename).read_text()).get("screens", []))
rows_by_slug = {row["slug"]: row for row in rows}

kind_keys = (
    ("button", "buttons"),
    ("input", "inputs"),
    ("tab", "tabs"),
    ("column", "columns"),
)
kind_totals = Counter()
scope_totals = Counter()
screen_blocks = []
missing = []

for seed in manifest["screens"]:
    slug = seed["slug"]
    row = rows_by_slug.get(slug)
    if not row or not row.get("domSummary"):
        missing.append(slug)
        continue
    summary = row["domSummary"]
    catalog_path = str(Path(filename).relative_to(ROOT))
    # The loop variable points at the final catalog; recover the actual source
    # path by matching the slug when possible.
    for candidate_name in glob.glob(CATALOG_GLOB):
        candidate = json.loads(Path(candidate_name).read_text()).get("screens", [])
        if any(item["slug"] == slug for item in candidate):
            catalog_path = str(Path(candidate_name).relative_to(ROOT))
            break
    interactions = []
    for kind, key in kind_keys:
        for label in unique(summary.get(key) or []):
            if kind == "button" and label in SHELL_BUTTONS:
                scope = "shell"
            elif kind == "button" and label in WELCOME_BUTTONS:
                scope = "welcome-banner"
            elif kind == "input" and label in {"checkbox"}:
                scope = "welcome-banner"
            else:
                scope = "page"
            interactions.append({
                "id": f"{slug}.{kind}.{len(interactions) + 1:03d}",
                "kind": kind,
                "scope": scope,
                "label": label,
            })
            kind_totals[kind] += 1
            scope_totals[scope] += 1
    lines = [
        f"  - id: {yq(slug)}",
        f"    route: {yq(seed['path'])}",
        f"    seed_screen: {yq(seed.get('seed') or 'none')}",
        f"    effort: {yq(seed.get('effort') or 'unknown')}",
        f"    evidence:",
        f"      screenshot: {yq('docs/reverse/frontend/parity-capture/' + row['screenshot'] if row.get('screenshot') else 'duplicate-of:' + str(row.get('duplicateOf') or 'none'))}",
        f"      dom_catalog: {yq(catalog_path)}",
        f"    observed_counts: {{ buttons: {len(unique(summary.get('buttons') or []))}, inputs: {len(unique(summary.get('inputs') or []))}, tabs: {len(unique(summary.get('tabs') or []))}, columns: {len(unique(summary.get('columns') or [])) }, total: {len(interactions)} }}",
        "    interactions: []" if not interactions else "    interactions:",
    ]
    for item in interactions:
        lines.append(f"      - id: {yq(item['id'])}")
        lines.append(f"        kind: {yq(item['kind'])}")
        lines.append(f"        scope: {yq(item['scope'])}")
        lines.append(f"        label: {yq(item['label'])}")
        lines.append("        implementation: not-mapped")
    screen_blocks.append("\n".join(lines))

total = sum(kind_totals.values())
header = [
    "# Artifactory interaction inventory v2 (generated)",
    "#",
    "# Evidence-only inventory generated from the 2026-09-18 catalog-v2 DOM summaries.",
    "# Labels are observed accessibility/DOM text, not inferred behavior. A label being",
    "# listed here does not claim that BinFlow implements it; implementation remains",
    "# not-mapped until a route/component/API/test mapping is added.",
    "meta:",
    f"  generated_from: {yq(str(MANIFEST.relative_to(ROOT)))}",
    "  catalog_glob: docs/reverse/frontend/parity-capture/catalog-v2-20260918/*/screens-catalog.json",
    f"  manifest_screens: {len(manifest['screens'])}",
    f"  inventoried_screens: {len(screen_blocks)}",
    f"  excluded_screens: [{', '.join(yq(x) for x in missing)}]",
    f"  unique_interactions: {total}",
    f"  by_kind: {{ buttons: {kind_totals['button']}, inputs: {kind_totals['input']}, tabs: {kind_totals['tab']}, columns: {kind_totals['column']} }}",
    f"  by_scope: {{ shell: {scope_totals['shell']}, welcome_banner: {scope_totals['welcome-banner']}, page: {scope_totals['page']} }}",
    "  implementation_coverage: { mapped: 0, total: " + str(total) + " }",
    "screens:",
]
OUTPUT.write_text("\n".join(header + screen_blocks) + "\n")
print(f"interaction inventory: {len(screen_blocks)} screens / {total} unique affordances -> {OUTPUT.relative_to(ROOT)}")
