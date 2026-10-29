"""Executable contract checks for the OpenAPI preparation step."""

from scripts.prepare_go_schema import dereference
from scripts.prepare_go_schema import prepare
from scripts.prepare_go_schema import subset
import copy
import pytest


def test_hoisted_definitions_keep_references_and_constraints():
    source = {
        "openapi": "3.1.0",
        "paths": {
            "/test": {
                "post": {
                    "$defs": {
                        "Node": {
                            "type": "object",
                            "additionalProperties": False,
                            "required": ["value"],
                            "properties": {
                                "value": {"type": ["string", "null"], "minLength": 2},
                                "next": {"$ref": "#/paths/~1test/post/$defs/Node"},
                            },
                        }
                    },
                    "schema": {"$ref": "#/paths/~1test/post/$defs/Node"},
                }
            }
        },
    }
    original = copy.deepcopy(source)
    result = prepare(source)
    assert source == original
    assert result["openapi"] == "3.1.0"
    reference = result["paths"]["/test"]["post"]["schema"]["$ref"]
    assert reference.startswith("#/components/schemas/")
    node = dereference(result, reference)
    assert node["additionalProperties"] is False
    assert node["required"] == ["value"]
    assert node["properties"]["value"] == {"type": ["string", "null"], "minLength": 2}
    assert node["properties"]["next"]["$ref"] == reference
    assert prepare(source) == result


def test_preparation_preserves_unions_enums_and_explicit_openness():
    source = {
        "components": {
            "schemas": {
                "Value": {"oneOf": [{"type": "integer"}, {"type": "string", "enum": ["auto"]}]},
                "Number": {"type": "number", "minimum": 0},
                "Open": {"type": "object", "properties": {"x": {"type": "string"}}},
                "Map": {"type": "object", "additionalProperties": {"type": "integer"}},
            }
        }
    }
    schemas = prepare(source)["components"]["schemas"]
    assert schemas["Value"] == source["components"]["schemas"]["Value"]
    assert schemas["Number"] == {"type": "number", "minimum": 0, "x-go-type": "float64"}
    assert schemas["Open"]["additionalProperties"] is True
    assert schemas["Map"] == source["components"]["schemas"]["Map"]


def test_subset_follows_transitive_and_recursive_references():
    source = {
        "paths": {
            "/keep": {"get": {"operationId": "keep", "schema": {"$ref": "#/components/schemas/A"}}},
            "/drop": {"get": {"operationId": "drop", "schema": {"$ref": "#/components/schemas/C"}}},
        },
        "components": {
            "schemas": {
                "A": {"properties": {"child": {"$ref": "#/components/schemas/B"}}},
                "B": {"properties": {"parent": {"$ref": "#/components/schemas/A"}}},
                "C": {"type": "string"},
            }
        },
    }
    result = subset(source, ["keep"])
    assert set(result["paths"]) == {"/keep"}
    assert set(result["components"]["schemas"]) == {"A", "B"}
    assert result["components"]["schemas"]["A"] == source["components"]["schemas"]["A"]
    with pytest.raises(ValueError, match="Unknown operation"):
        subset(source, ["missing"])
