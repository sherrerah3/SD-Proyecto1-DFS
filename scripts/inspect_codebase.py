"""Inspect the dfsha codebase structure and Go function dependencies.

The script locates the dfsha project root through ``go.mod`` and inspects
a component selected with the path:

    internal/<x>/<y>/

The values of ``x`` and ``y`` are provided through the command line, so the
script does not assume that ``client`` is the only directory under
``internal``.

For the ``shell`` component, Go files are grouped logically into:

    HANDLERS
    SESSION
    CORE
    OTHER

These groups are only labels for the terminal output. The script does not
create or assume that these directories exist.

The script also analyzes direct dependencies between functions defined
inside the selected Go component.

Examples:

    python scripts/inspect_codebase.py client shell
    python scripts/inspect_codebase.py client http
    python scripts/inspect_codebase.py metanode rpc
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path


PROJECT_NAME = "dfsha"
PROJECT_MARKER = "go.mod"

INTERNAL_DIRECTORY = "internal"

GO_EXTENSION = ".go"

SHELL_COMPONENT = "shell"

HANDLER_PREFIX = "handle_"
HANDLER_COMMAND_FILE = "handle_command"

SESSION_SUFFIX = "_session"
SESSION_PATH_FILE = "get_session_path"

GROUP_HANDLERS = "HANDLERS"
GROUP_SESSION = "SESSION"
GROUP_CORE = "CORE"
GROUP_OTHER = "OTHER"

FUNCTIONS_WITH_DEPENDENCIES = "FUNCTIONS WITH DEPENDENCIES"
FUNCTIONS_WITHOUT_DEPENDENCIES = (
    "FUNCTIONS WITHOUT DEPENDENCIES"
)

TARGET_ALL = "all"

FUNCTION_DECLARATION_PATTERN = re.compile(
    r"(?m)^[ \t]*func[ \t]+"
    r"(?:\([^)]*\)[ \t]+)?"
    r"([A-Za-z_][A-Za-z0-9_]*)[ \t]*\("
)

FUNCTION_CALL_PATTERN = re.compile(
    r"\b([A-Za-z_][A-Za-z0-9_]*)[ \t]*\("
)


class FunctionInfo:
    """Store information about one Go function."""

    def __init__(
        self,
        name: str,
        file_name: str,
        dependencies: list[str],
    ) -> None:
        """Initialize function information."""
        self.name = name
        self.file_name = file_name
        self.dependencies = dependencies


def main() -> int:
    """Inspect one internal/x/y component."""
    arguments = parse_arguments()

    project_root = find_project_root()

    if project_root is None:
        print(
            f"error: {PROJECT_MARKER} was not found",
            file=sys.stderr,
        )
        return 1

    component_path = get_component_path(
        project_root,
        arguments.x,
        arguments.y,
    )

    if not component_path.is_dir():
        print(
            "error: component directory was not found: "
            f"{component_path}",
            file=sys.stderr,
        )
        return 1

    try:
        files = find_go_files(component_path)

        print_component_structure(
            project_root,
            arguments.x,
            arguments.y,
            component_path,
            files,
        )

        if arguments.y.lower() == SHELL_COMPONENT:
            print_component_analysis(
                component_path,
                files,
            )

    except RuntimeError as error:
        print(
            f"error: {error}",
            file=sys.stderr,
        )
        return 1

    return 0


def parse_arguments() -> argparse.Namespace:
    """Parse the internal path components selected by the user."""
    parser = argparse.ArgumentParser(
        description=(
            f"Inspect {PROJECT_NAME} code under "
            "internal/x/y."
        ),
    )

    parser.add_argument(
        "x",
        help="first directory under internal",
    )

    parser.add_argument(
        "y",
        help="selected component under internal/x",
    )

    return parser.parse_args()


def find_project_root() -> Path | None:
    """Find the project root by searching for go.mod."""
    script_path = Path(__file__).resolve()

    for path in (
        script_path.parent,
        *script_path.parent.parents,
    ):
        if (path / PROJECT_MARKER).is_file():
            return path

    return None


def get_component_path(
    project_root: Path,
    x: str,
    y: str,
) -> Path:
    """Build the selected internal/x/y path."""
    return (
        project_root
        / INTERNAL_DIRECTORY
        / x
        / y
    )


def find_go_files(
    directory: Path,
) -> list[Path]:
    """Return Go files directly contained in a directory."""
    try:
        entries = list(directory.iterdir())
    except OSError as error:
        raise RuntimeError(
            f"failed to read {directory}: {error}"
        ) from error

    files = [
        entry
        for entry in entries
        if entry.is_file()
        and entry.suffix == GO_EXTENSION
    ]

    return sorted(
        files,
        key=lambda path: path.name.lower(),
    )


def print_component_structure(
    project_root: Path,
    x: str,
    y: str,
    component_path: Path,
    files: list[Path],
) -> None:
    """Print the selected internal/x/y structure."""
    print(f"{PROJECT_NAME}/")
    print(f"└── {INTERNAL_DIRECTORY}/")
    print(f"    └── {x}/")
    print(f"        └── {y}/")

    if not files:
        print("            └── (no Go files)")
        print()
        return

    if y.lower() == SHELL_COMPONENT:
        print_shell_structure(files)
    else:
        print_generic_structure(files)

    print()


def print_generic_structure(
    files: list[Path],
) -> None:
    """Print files for a generic component."""
    for index, file in enumerate(files):
        last_file = index == len(files) - 1

        prefix = (
            "└── "
            if last_file
            else "├── "
        )

        print(
            f"            {prefix}"
            f"{file.name}"
        )


def print_shell_structure(
    files: list[Path],
) -> None:
    """Print shell files grouped by responsibility."""
    groups = build_shell_groups(files)

    group_names = (
        GROUP_HANDLERS,
        GROUP_SESSION,
        GROUP_CORE,
        GROUP_OTHER,
    )

    visible_groups = [
        group_name
        for group_name in group_names
        if groups[group_name]
    ]

    for group_index, group_name in enumerate(
        visible_groups,
    ):
        last_group = (
            group_index
            == len(visible_groups) - 1
        )

        group_prefix = (
            "└── "
            if last_group
            else "├── "
        )

        print(
            f"            {group_prefix}"
            f"[{group_name}]"
        )

        files_in_group = groups[group_name]

        for file_index, file in enumerate(
            files_in_group,
        ):
            last_file = (
                file_index
                == len(files_in_group) - 1
            )

            file_prefix = (
                "└── "
                if last_file
                else "├── "
            )

            group_indent = (
                "    "
                if last_group
                else "│   "
            )

            print(
                f"            {group_indent}"
                f"{file_prefix}"
                f"{file.name}"
            )


def classify_shell_file(
    file: Path,
) -> str:
    """Classify a shell file by its naming convention."""
    base_name = file.stem

    if (
        base_name == HANDLER_COMMAND_FILE
        or base_name.startswith(HANDLER_PREFIX)
    ):
        return GROUP_HANDLERS

    if (
        base_name.endswith(SESSION_SUFFIX)
        or base_name == SESSION_PATH_FILE
    ):
        return GROUP_SESSION

    if "_" not in base_name:
        return GROUP_CORE

    return GROUP_OTHER


def build_shell_groups(
    files: list[Path],
) -> dict[str, list[Path]]:
    """Group shell files by their logical responsibility."""
    groups = {
        GROUP_HANDLERS: [],
        GROUP_SESSION: [],
        GROUP_CORE: [],
        GROUP_OTHER: [],
    }

    for file in files:
        group_name = classify_shell_file(file)
        groups[group_name].append(file)

    for group_files in groups.values():
        group_files.sort(
            key=lambda path: path.name.lower(),
        )

    return groups


def print_component_analysis(
    component_path: Path,
    files: list[Path],
) -> None:
    """Print function relationships for the selected component."""
    functions = analyze_functions(files)

    if not functions:
        return

    print("FUNCTION ANALYSIS")
    print()

    print(
        f"└── {component_path.as_posix()}/"
    )

    print_functions_with_dependencies(
        functions,
    )

    print_functions_without_dependencies(
        functions,
    )

    print()


def analyze_functions(
    files: list[Path],
) -> list[FunctionInfo]:
    """Analyze local Go functions and direct dependencies."""
    sources: dict[Path, str] = {}
    function_names: set[str] = set()

    for file in files:
        try:
            source = file.read_text(
                encoding="utf-8",
            )
        except OSError as error:
            raise RuntimeError(
                f"failed to read {file}: {error}"
            ) from error

        sources[file] = source

        for match in FUNCTION_DECLARATION_PATTERN.finditer(
            source,
        ):
            function_names.add(
                match.group(1),
            )

    functions: list[FunctionInfo] = []

    for file, source in sources.items():
        matches = FUNCTION_DECLARATION_PATTERN.finditer(
            source,
        )

        for match in matches:
            function_name = match.group(1)

            body = extract_function_body(
                source,
                match.end(),
            )

            if body is None:
                raise RuntimeError(
                    f"failed to locate body of "
                    f"{function_name} in "
                    f"{file.name}"
                )

            dependencies = find_function_dependencies(
                body,
                function_name,
                function_names,
            )

            functions.append(
                FunctionInfo(
                    name=function_name,
                    file_name=file.name,
                    dependencies=dependencies,
                ),
            )

    return sorted(
        functions,
        key=lambda function: (
            function.name.lower(),
            function.file_name.lower(),
        ),
    )


def extract_function_body(
    source: str,
    start_index: int,
) -> str | None:
    """Extract one Go function body."""
    opening_brace = source.find(
        "{",
        start_index,
    )

    if opening_brace == -1:
        return None

    depth = 0
    index = opening_brace

    while index < len(source):
        character = source[index]

        if character == '"':
            index = skip_quoted_string(
                source,
                index,
            )
            continue

        if character == "`":
            index = skip_raw_string(
                source,
                index,
            )
            continue

        if character == "'":
            index = skip_rune(
                source,
                index,
            )
            continue

        if (
            character == "/"
            and index + 1 < len(source)
        ):
            next_character = source[index + 1]

            if next_character == "/":
                index = skip_line_comment(
                    source,
                    index,
                )
                continue

            if next_character == "*":
                index = skip_block_comment(
                    source,
                    index,
                )
                continue

        if character == "{":
            depth += 1

        elif character == "}":
            depth -= 1

            if depth == 0:
                return source[
                    opening_brace + 1:index
                ]

        index += 1

    return None


def skip_quoted_string(
    source: str,
    start_index: int,
) -> int:
    """Skip a double-quoted Go string."""
    index = start_index + 1

    while index < len(source):
        character = source[index]

        if character == "\\":
            index += 2
            continue

        if character == '"':
            return index + 1

        index += 1

    return len(source)


def skip_raw_string(
    source: str,
    start_index: int,
) -> int:
    """Skip a raw Go string."""
    index = start_index + 1

    while index < len(source):
        if source[index] == "`":
            return index + 1

        index += 1

    return len(source)


def skip_rune(
    source: str,
    start_index: int,
) -> int:
    """Skip a Go rune literal."""
    index = start_index + 1

    while index < len(source):
        character = source[index]

        if character == "\\":
            index += 2
            continue

        if character == "'":
            return index + 1

        index += 1

    return len(source)


def skip_line_comment(
    source: str,
    start_index: int,
) -> int:
    """Skip a Go line comment."""
    newline_index = source.find(
        "\n",
        start_index + 2,
    )

    if newline_index == -1:
        return len(source)

    return newline_index + 1


def skip_block_comment(
    source: str,
    start_index: int,
) -> int:
    """Skip a Go block comment."""
    end_index = source.find(
        "*/",
        start_index + 2,
    )

    if end_index == -1:
        return len(source)

    return end_index + 2


def find_function_dependencies(
    body: str,
    current_function: str,
    function_names: set[str],
) -> list[str]:
    """Find local functions called by another local function."""
    clean_body = remove_strings_and_comments(
        body,
    )

    dependencies: set[str] = set()

    for match in FUNCTION_CALL_PATTERN.finditer(
        clean_body,
    ):
        function_name = match.group(1)

        if function_name == current_function:
            continue

        if function_name not in function_names:
            continue

        dependencies.add(function_name)

    return sorted(
        dependencies,
        key=str.lower,
    )


def remove_strings_and_comments(
    source: str,
) -> str:
    """Remove strings and comments from Go source."""
    result: list[str] = []
    index = 0

    while index < len(source):
        character = source[index]

        if character == '"':
            end_index = skip_quoted_string(
                source,
                index,
            )

            result.append(
                " " * (end_index - index)
            )

            index = end_index
            continue

        if character == "`":
            end_index = skip_raw_string(
                source,
                index,
            )

            result.append(
                " " * (end_index - index)
            )

            index = end_index
            continue

        if character == "'":
            end_index = skip_rune(
                source,
                index,
            )

            result.append(
                " " * (end_index - index)
            )

            index = end_index
            continue

        if (
            character == "/"
            and index + 1 < len(source)
        ):
            next_character = source[index + 1]

            if next_character == "/":
                end_index = skip_line_comment(
                    source,
                    index,
                )

                result.append(
                    "\n"
                    * source[index:end_index].count(
                        "\n",
                    )
                )

                index = end_index
                continue

            if next_character == "*":
                end_index = skip_block_comment(
                    source,
                    index,
                )

                result.append(
                    "\n"
                    * source[index:end_index].count(
                        "\n",
                    )
                )

                index = end_index
                continue

        result.append(character)
        index += 1

    return "".join(result)


def print_functions_with_dependencies(
    functions: list[FunctionInfo],
) -> None:
    """Print functions that have local dependencies."""
    dependent_functions = [
        function
        for function in functions
        if function.dependencies
    ]

    if not dependent_functions:
        return

    print(
        "    ├── "
        f"{FUNCTIONS_WITH_DEPENDENCIES}"
    )

    for function_index, function in enumerate(
        dependent_functions,
    ):
        last_function = (
            function_index
            == len(dependent_functions) - 1
        )

        function_prefix = (
            "└── "
            if last_function
            else "├── "
        )

        print(
            "    │   "
            f"{function_prefix}"
            f"{function.name} "
            f"({function.file_name})"
        )

        dependency_indent = (
            "        "
            if last_function
            else "    │   "
        )

        for dependency_index, dependency in enumerate(
            function.dependencies,
        ):
            last_dependency = (
                dependency_index
                == len(function.dependencies) - 1
            )

            dependency_prefix = (
                "└── "
                if last_dependency
                else "├── "
            )

            print(
                "    │   "
                f"{dependency_indent}"
                f"{dependency_prefix}"
                f"{dependency}"
            )


def print_functions_without_dependencies(
    functions: list[FunctionInfo],
) -> None:
    """Print functions with no local dependencies."""
    independent_functions = [
        function
        for function in functions
        if not function.dependencies
    ]

    if not independent_functions:
        return

    print(
        "    └── "
        f"{FUNCTIONS_WITHOUT_DEPENDENCIES}"
    )

    for index, function in enumerate(
        independent_functions,
    ):
        last_function = (
            index
            == len(independent_functions) - 1
        )

        prefix = (
            "└── "
            if last_function
            else "├── "
        )

        print(
            "        "
            f"{prefix}"
            f"{function.name} "
            f"({function.file_name})"
        )


if __name__ == "__main__":
    raise SystemExit(main())