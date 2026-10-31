"""Generated help offsets must address UTF-8 bytes, not character positions."""

from scripts.generate_go_doc_metadata import help_index
import json
import re


def test_help_index_handles_unicode_and_nested_terminal_keys():
    specs = {
        "avr": {"help": '雪 🦫 and quoted "terminal": {}'},
        "avr repo list": {"help": "café", "options": [{"label": "--json"}]},
    }
    source = json.dumps(
        {"nested": {"terminal": {"wrong": {}}}, "prose": "🦫", "terminal": specs}, indent=2, ensure_ascii=False
    )
    generated = help_index(source)
    entries = re.findall(r'case ("[^"]+"):\n\s*return metadata\[(\d+):(\d+)\]', generated)
    actual = {
        json.loads(name): json.loads(source.encode("utf-8")[int(begin) : int(end)]) for name, begin, end in entries
    }
    assert actual == specs
