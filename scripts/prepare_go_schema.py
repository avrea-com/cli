"""Prepare OpenAPI 3.1 for Go generation without downgrading its contract.

oapi-codegen v2.8.1 cannot name references into nested $defs. Hoist only
referenced definitions into components, retaining the original schemas.
Explicit additionalProperties preserves JSON Schema's default openness;
x-go-type selects float64 instead of the generator's float32 default.
"""

from pathlib import Path
from typing import Any
import argparse
import copy
import hashlib
import json
import re


def walk(value: Any):
    if isinstance(value, dict):
        yield value
        for child in list(value.values()):
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)


def dereference(document: dict, reference: str) -> Any:
    if not reference.startswith("#/"):
        raise ValueError(f"Only local schema references are supported: {reference}")
    value: Any = document
    for part in reference[2:].split("/"):
        key = part.replace("~1", "/").replace("~0", "~")
        value = value[int(key)] if isinstance(value, list) else value[key]
    return value


def prepare(document: dict) -> dict:
    result = copy.deepcopy(document)
    components = result.setdefault("components", {}).setdefault("schemas", {})
    # Discover references against the untouched input so hoisting never changes
    # how a nested reference resolves. Stable names avoid order-dependent output.
    references = sorted(
        {node["$ref"] for node in walk(document) if isinstance(node.get("$ref"), str) and "/$defs/" in node["$ref"]}
    )
    replacements = {}
    for reference in references:
        target = dereference(document, reference)
        title = re.sub(r"[^A-Za-z0-9]", "", target.get("title", "Definition"))
        name = "Inline" + title + hashlib.sha256(reference.encode()).hexdigest()[:10]
        if name in components:
            raise ValueError(f"Hoisted schema name collision: {name}")
        components[name] = copy.deepcopy(target)
        replacements[reference] = f"#/components/schemas/{name}"
    for node in walk(result):
        if node.get("$ref") in replacements:
            node["$ref"] = replacements[node["$ref"]]
        if node.get("type") == "number" and "format" not in node:
            node.setdefault("x-go-type", "float64")
        if node.get("type") == "object" and node.get("properties") and "additionalProperties" not in node:
            node["additionalProperties"] = True
    return result


def subset(document: dict, operations: list[str]) -> dict:
    """Keep selected operations and their transitive schema references."""
    result = copy.deepcopy(document)
    selected = set(operations)
    found = set()
    paths = {}
    for path, item in result["paths"].items():
        methods = {}
        for method, operation in item.items():
            if isinstance(operation, dict) and operation.get("operationId") in selected:
                methods[method] = operation
                found.add(operation["operationId"])
        if methods:
            for key in ("parameters", "servers"):
                if key in item:
                    methods[key] = item[key]
            paths[path] = methods
    if missing := selected - found:
        raise ValueError(f"Unknown operation IDs: {sorted(missing)}")
    result["paths"] = paths
    needed = {}
    pending = list(walk(paths))
    while pending:
        node = pending.pop()
        reference = node.get("$ref", "")
        if reference.startswith("#/components/schemas/"):
            name = reference.removeprefix("#/components/schemas/")
            if name not in needed:
                needed[name] = copy.deepcopy(dereference(document, reference))
                pending.extend(walk(needed[name]))
    result["components"]["schemas"] = needed
    return result


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("destination", type=Path)
    parser.add_argument("--operations", nargs="+")
    args = parser.parse_args()
    document = json.loads(args.source.read_text())
    if document.get("openapi") != "3.1.0":
        raise ValueError("Review the generator gate before changing OpenAPI versions")
    document = prepare(document)
    if args.operations:
        document = subset(document, args.operations)
    args.destination.parent.mkdir(parents=True, exist_ok=True)
    args.destination.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n")


if __name__ == "__main__":
    main()
