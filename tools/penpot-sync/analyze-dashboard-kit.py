#!/usr/bin/env python3
"""Audit the official Penpot Dashboard UI Kit export.

Usage: analyze-dashboard-kit.py <dashboard.penpot> <output.json>
The .penpot source is a zip exported by Penpot; this script is read-only.
"""
import collections
import io
import json
import sys
import zipfile
from pathlib import Path

SOURCE_URL = "https://penpot.github.io/penpot-files/Dashboard%20UI%20Kit%20-%20Dashboard%2C%20Free%20Admin%20Dashboard%20(Community).penpot"
ZERO = "00000000-0000-0000-0000-000000000000"


def color_value(fill):
    if not isinstance(fill, dict):
        return None
    if isinstance(fill.get("fillColor"), str):
        return fill["fillColor"].lower()
    if isinstance(fill.get("color"), str):
        return fill["color"].lower()
    if isinstance(fill.get("gradient"), dict):
        return "gradient"
    return None


def opacity_value(style, prefix="fillOpacity"):
    if not isinstance(style, dict):
        return None
    value = style.get(prefix)
    if isinstance(value, (int, float)):
        return round(float(value), 3)
    if isinstance(style.get("color"), dict):
        return round(float(style["color"].get("opacity", 1)), 3)
    return None


def bump(counter, key, **extra):
    item = counter[key]
    item["count"] += 1
    for k, v in extra.items():
        if v is not None:
            item.setdefault(k, v)


def named_counter():
    return collections.defaultdict(lambda: {"count": 0})


def sorted_counter(counter, limit=None):
    rows = sorted(counter.items(), key=lambda kv: (-kv[1]["count"], kv[0]))
    selected = rows[:limit] if limit is not None else rows
    return [dict(value or {}, **{"value": key}) for key, value in selected]


def walk_text_nodes(value):
    if isinstance(value, dict):
        if isinstance(value.get("text"), str) and any(k in value for k in ("fontFamily", "fontSize", "fontWeight")):
            yield value
        for child in value.values():
            yield from walk_text_nodes(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk_text_nodes(child)


def main():
    archive_path, output_path = sys.argv[1:]
    with zipfile.ZipFile(archive_path) as zf:
        manifest = json.loads(zf.read("manifest.json"))
        file_entry = manifest["files"][0]
        file_id = file_entry["id"]
        prefix = f"files/{file_id}/pages/"
        page_names = []
        for name in zf.namelist():
            if not name.startswith(prefix) or not name.endswith(".json"):
                continue
            parts = name[len(prefix):].split("/")
            if len(parts) == 1:
                page = json.loads(zf.read(name))
                page_names.append({"id": page["id"], "name": page["name"], "archivePath": name})

        objects = []
        page_dirs = {str(Path(p["archivePath"]).parent) for p in page_names}
        for page_dir in sorted(page_dirs):
            for name in zf.namelist():
                if not name.startswith(page_dir + "/") or not name.endswith(".json"):
                    continue
                if name == f"{page_dir}/{Path(page_dir).name}.json":
                    continue
                try:
                    obj = json.loads(zf.read(name))
                except json.JSONDecodeError:
                    continue
                if isinstance(obj, dict) and isinstance(obj.get("id"), str):
                    obj["_archivePath"] = name
                    objects.append(obj)

    by_id = {o["id"]: o for o in objects}
    parent = {o["id"]: o.get("parentId") for o in objects}

    def root_of(obj_id):
        seen = set()
        current = obj_id
        while current in by_id and current not in seen:
            seen.add(current)
            next_id = parent.get(current)
            if not next_id or next_id == ZERO:
                return current
            current = next_id
        return current

    roots = [o for o in objects if o.get("parentId") == ZERO]
    boards = []
    for o in roots:
        if o["id"] == ZERO:
            continue
        boards.append({
            "id": o["id"], "name": o.get("name"), "type": o.get("type"),
            "x": round(o.get("x") or 0), "y": round(o.get("y") or 0),
            "width": round(o.get("width") or 0), "height": round(o.get("height") or 0),
            "childCount": len(o.get("shapes") or []),
            "componentRoot": bool(o.get("componentRoot")),
        })

    fill_colors = named_counter()
    text_colors = named_counter()
    stroke_colors = named_counter()
    shadows = named_counter()
    radii = named_counter()
    gaps = named_counter()
    paddings = named_counter()
    typography = named_counter()
    font_families = named_counter()
    layouts = named_counter()
    components = {}
    component_instances = collections.Counter()

    for o in objects:
        root_id = root_of(o["id"])
        root_name = by_id.get(root_id, {}).get("name")
        for fill in o.get("fills") or []:
            c = color_value(fill)
            if c:
                bump(fill_colors, c, opacity=opacity_value(fill), root=root_name)
        for stroke in o.get("strokes") or []:
            c = color_value(stroke) or (stroke.get("strokeColor") or "").lower()
            if c:
                bump(stroke_colors, c, opacity=opacity_value(stroke, "strokeOpacity"), width=stroke.get("strokeWidth"), root=root_name)
        for shadow in o.get("shadow") or []:
            c = (shadow.get("color") or {}).get("color", "").lower()
            key = f"{c or 'none'} / {shadow.get('style')} / x={shadow.get('offsetX')} y={shadow.get('offsetY')} blur={shadow.get('blur')} spread={shadow.get('spread')}"
            bump(shadows, key, opacity=(shadow.get("color") or {}).get("opacity"), root=root_name)
        rr = [o.get(k) for k in ("r1", "r2", "r3", "r4")]
        if any(isinstance(v, (int, float)) for v in rr):
            bump(radii, " / ".join(str(v) for v in rr), root=root_name)
        gap = o.get("layoutGap")
        if isinstance(gap, dict):
            bump(gaps, f"row={gap.get('rowGap')} column={gap.get('columnGap')}", root=root_name)
        pad = o.get("layoutPadding")
        if isinstance(pad, dict) and any(pad.get(k) not in (None, 0) for k in ("p1", "p2", "p3", "p4")):
            bump(paddings, "top={p1} right={p2} bottom={p3} left={p4}".format(**pad), root=root_name)
        if o.get("layout"):
            key = f"{o.get('layout')} / {o.get('layoutFlexDir')} / align={o.get('layoutAlignItems')} / justify={o.get('layoutJustifyContent')}"
            bump(layouts, key, root=root_name)
        if o.get("componentRoot"):
            key = o.get("componentId") or o["id"]
            item = components.setdefault(key, {
                "componentId": key,
                "name": o.get("name"),
                "definitionIds": [],
                "instanceCount": 0,
                "width": round(o.get("width") or 0),
                "height": round(o.get("height") or 0),
            })
            item["definitionIds"].append(o["id"])
        elif o.get("componentId"):
            component_instances[o["componentId"]] += 1
        if o.get("type") == "text":
            for node in walk_text_nodes(o.get("content")):
                family = node.get("fontFamily")
                if family:
                    bump(font_families, family)
                typo_key = " / ".join(str(node.get(k) or "-") for k in (
                    "fontFamily", "fontSize", "fontWeight", "lineHeight", "letterSpacing", "textAlign", "textTransform", "textDecoration"
                ))
                bump(typography, typo_key, root=root_name)
                for fill in node.get("fills") or []:
                    c = color_value(fill)
                    if c:
                        bump(text_colors, c, opacity=opacity_value(fill), root=root_name)

    for component_id, count in component_instances.items():
        if component_id in components:
            components[component_id]["instanceCount"] = count

    token_file = f"files/{file_id}/tokens.json"
    with zipfile.ZipFile(archive_path) as zf:
        tokens = json.loads(zf.read(token_file)) if token_file in zf.namelist() else None

    result = {
        "meta": {
            "source": SOURCE_URL,
            "sourceName": file_entry["name"],
            "sourceFileId": file_id,
            "features": file_entry.get("features", []),
            "objectCount": len(objects),
            "boardCount": len(boards),
            "componentDefinitionCount": len(components),
            "componentInstanceCount": sum(component_instances.values()),
        },
        "pages": page_names,
        "boards": sorted(boards, key=lambda b: (b["y"], b["x"])),
        "penpotTokens": tokens,
        "derivedTokens": {
            "fillColors": sorted_counter(fill_colors),
            "textColors": sorted_counter(text_colors),
            "strokeColors": sorted_counter(stroke_colors),
            "shadows": sorted_counter(shadows),
            "radii": sorted_counter(radii),
            "gaps": sorted_counter(gaps),
            "paddings": sorted_counter(paddings),
            "typography": sorted_counter(typography),
            "fontFamilies": sorted_counter(font_families),
            "layouts": sorted_counter(layouts),
        },
        "components": sorted(components.values(), key=lambda c: (-c["instanceCount"], c["name"] or "", c["componentId"])),
    }
    Path(output_path).write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    main()
