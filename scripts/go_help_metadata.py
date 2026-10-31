"""Export terminal-help metadata from the public CLI without invoking commands."""

from avrea_cli.click_ext import GhHelpMixin
from avrea_cli.main import cli
import click


class Rows(click.HelpFormatter):
    def __init__(self):
        super().__init__(width=78)
        self.rows = []

    def write_dl(self, rows, *args, **kwargs):
        self.rows.extend(list(rows))


def terminal_tree():
    result = {}

    def walk(command, path, parent=None):
        settings = dict(command.context_settings)
        settings["terminal_width"] = 78
        context = click.Context(command, info_name=command.name, parent=parent, **settings)
        # The docs walker creates parentless contexts and Click caches its help
        # option. Recreate it here with the actual inherited root settings.
        command._help_option = None
        options = []
        completion = []
        for parameter in command.get_params(context):
            kind = "option" if isinstance(parameter, click.Option) else "argument"
            completion.append(
                {
                    "name": parameter.name,
                    "kind": kind,
                    "names": parameter.opts + parameter.secondary_opts,
                    "help": getattr(parameter, "help", None) or "",
                    "multiple": parameter.multiple,
                    "flag": bool(getattr(parameter, "is_flag", False)),
                    "hidden": bool(getattr(parameter, "hidden", False)),
                    "nargs": parameter.nargs,
                    "type": {
                        key: value
                        for key, value in parameter.type.to_info_dict().items()
                        if key in {"param_type", "choices", "case_sensitive", "file_okay"}
                    },
                }
            )
            record = parameter.get_help_record(context)
            if record is not None:
                options.append(
                    {
                        "names": parameter.opts + parameter.secondary_opts,
                        "label": record[0],
                        "description": record[1],
                    }
                )
        rows = Rows()
        if isinstance(command, click.Group):
            command.format_commands(context, rows)
        descriptions = {name.removesuffix(":"): description for name, description in rows.rows}
        children = []
        if isinstance(command, click.Group):
            for name in command.list_commands(context):
                child = command.get_command(context, name)
                if child is None:
                    continue
                if name in descriptions:
                    children.append(
                        {
                            "name": name,
                            "description": descriptions[name],
                            "section": getattr(child, "help_section", None),
                        }
                    )
                walk(child, path + " " + name, context)
        result[path] = {
            "completion_help": command.get_short_help_str(),
            "completion_params": completion,
            "sections": isinstance(command, GhHelpMixin),
            "help": command.help or "",
            "epilog": command.epilog or "",
            "usage": command.collect_usage_pieces(context),
            "options": options,
            "commands": children,
            "no_args_is_help": bool(command.no_args_is_help),
            "section_order": getattr(command, "_section_order", []),
        }

    walk(cli, "avr")
    return result
