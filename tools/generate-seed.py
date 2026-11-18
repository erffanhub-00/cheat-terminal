#!/usr/bin/env python3
"""Generate data/seed.json for cheat-terminal from Erfan's English cheat sheets.

Usage:
    python3 tools/generate-seed.py [linux.md powershell.md docker.md git.md]

Defaults to the four EN cheat sheets in ~/workspace/gist-fixes (the live
gist contents, verified byte-identical). Output is data/seed.json next to
the repo root (or --out <path>).

What it extracts:
  * Markdown tables  -> first column = command, rest = description.
  * Fenced code blocks (bash/sh/powershell, or untagged) -> lines that look
    like commands; a trailing " # comment" becomes the description.

Skipped: Contents, Recommended Resources and Me sections, table header and
separator rows, shell keywords, variable assignments, comments, URLs.

Duplicate (category, command) pairs are kept once (first wins).
"""

import json
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent

DEFAULT_SOURCES = [
    ("linux", "Linux", REPO_ROOT.parent / "gist-fixes" / "linux-cheatsheet" / "linux-cheatsheet-en.md"),
    ("powershell", "PowerShell", REPO_ROOT.parent / "gist-fixes" / "powershell-cheatsheet" / "powershell-cheatsheet-en.md"),
    ("docker", "Docker", REPO_ROOT.parent / "gist-fixes" / "docker-cheatsheet" / "docker-cheatsheet-en.md"),
    ("git", "Git", REPO_ROOT.parent / "gist-fixes" / "fixed" / "git-cheatsheet.md"),
]

SKIP_SECTIONS = {"contents", "recommended resources", "me"}
SHELL_KEYWORDS = {
    "for", "if", "while", "until", "do", "done", "then", "else", "elif",
    "fi", "case", "esac", "function", "select", "{", "}",
}
CODE_BLOCK_LANGS = {"", "bash", "sh", "shell", "powershell", "ps1", "console", "text"}

# Usefulness levels: 1 = Essential (everyday commands), 2 = Common,
# 3 = Advanced (long or composed commands). The level is the "how useful
# is this command" filter in the TUI and CLI (--level).
LEVEL_ESSENTIAL = 1
LEVEL_COMMON = 2
LEVEL_ADVANCED = 3

# Base commands/verbs that count as essential, per category.
ESSENTIALS = {
    "linux": {
        "ls", "cd", "pwd", "cp", "mv", "rm", "mkdir", "cat", "less",
        "head", "tail", "grep", "find", "ps", "top", "kill",
        "df", "du", "free", "chmod", "chown", "ssh", "scp", "tar", "curl",
        "wget", "ping", "ip", "ss", "systemctl", "journalctl", "man",
        "which", "history", "clear", "exit", "echo", "touch",
        "nano", "vim", "vi", "sudo", "apt", "pacman", "env", "export",
        "alias", "source", "ln", "wc", "sort", "uniq", "cut", "awk", "sed",
        "tee", "xargs", "watch", "whoami", "hostname", "uname", "date",
        "uptime", "mount", "umount", "crontab", "tmux", "screen",
    },
    "git": {
        "status", "add", "commit", "push", "pull", "clone", "branch",
        "checkout", "switch", "restore", "log", "diff", "show", "stash",
        "merge", "rebase", "remote", "fetch", "reset", "revert", "tag",
        "blame", "config", "init", "mv", "rm", "clean", "cherry-pick",
        "worktree", "submodule", "gc", "reflog", "help",
    },
    "docker": {
        "ps", "images", "image", "run", "start", "stop", "restart", "rm",
        "rmi", "logs", "exec", "build", "pull", "push", "inspect", "cp",
        "network", "volume", "container", "compose", "system", "info",
        "version", "login", "tag", "top", "stats", "port", "create", "kill",
        "rename", "search",
    },
    "powershell": {
        "get-help", "get-command", "get-childitem", "set-location",
        "get-location", "get-content", "set-content", "add-content",
        "get-process", "stop-process", "start-process", "get-service",
        "start-service", "stop-service", "test-connection", "copy-item",
        "move-item", "remove-item", "rename-item", "new-item", "get-item",
        "test-path", "get-date", "clear-host", "get-history", "get-alias",
        "set-alias", "set-executionpolicy", "get-executionpolicy",
        "select-object", "where-object", "foreach-object", "sort-object",
        "measure-object", "out-file", "invoke-webrequest", "get-module",
        "import-module", "write-host", "write-output", "read-host",
        "get-member", "format-table", "format-list", "export-csv",
        "import-csv", "get-computerinfo", "get-disk", "get-childitem",
        "resolve-path", "join-path", "get-psdrive", "update-help",
        "get-variable", "set-variable", "start-sleep", "convertto-json",
        "convertfrom-json", "invoke-command", "enter-pssession",
    },
}


def base_of(command: str, category: str) -> str:
    """Normalized base command used for the essential check."""
    cmd = command.strip().lower()
    cmd = re.sub(r"^sudo\s+", "", cmd)
    tokens = cmd.split()
    if not tokens:
        return ""
    if category == "git" and tokens[0] == "git" and len(tokens) > 1:
        return tokens[1].strip("-")
    if category == "docker" and tokens[0] == "docker" and len(tokens) > 1:
        if tokens[1] == "compose" and len(tokens) > 2:
            return "compose"
        return tokens[1].strip("-")
    base = tokens[0].strip("`\"'")
    if "/" in base:
        base = base.rsplit("/", 1)[-1]
    return base


def level_of(command: str, category: str) -> int:
    """Usefulness level: 1 Essential, 2 Common, 3 Advanced."""
    cmd = command.strip()
    # Concept-table rows ("Image", "Container", ...) are not commands:
    # only real git/docker invocations can be essential.
    if category == "git" and not cmd.lower().startswith("git "):
        pass
    elif category == "docker" and not cmd.lower().startswith("docker "):
        pass
    # Essential = a well-known base command, used plainly (short form).
    elif base_of(cmd, category) in ESSENTIALS.get(category, set()) and len(cmd.split()) <= 3:
        return LEVEL_ESSENTIAL
    # Long or composed commands (pipes, chains, redirects, substitutions)
    # are advanced, whatever their base command is.
    if any(t in cmd for t in ("|", "&&", ";", "$(", "`", ">", "<")):
        return LEVEL_ADVANCED
    if len(cmd.split()) > 5:
        return LEVEL_ADVANCED
    if cmd[:1] in ("*", "[", "-"):
        return LEVEL_ADVANCED
    return LEVEL_COMMON


def slugify(text: str) -> str:
    text = re.sub(r"[^\w\s-]", "", text.lower())
    return re.sub(r"[\s_]+", "-", text).strip("-")


def clean_section(title: str) -> str:
    title = re.sub(r"^[\d]+[.)]\s*", "", title.strip())
    title = re.sub(r"^[^\w]+", "", title)  # leading emoji
    return title.strip()


def clean_cell(cell: str) -> str:
    cell = cell.strip()
    cell = cell.replace("\\|", "|")
    cell = re.sub(r"`([^`]*)`", r"\1", cell)          # inline code
    cell = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", cell)  # markdown links
    cell = cell.replace("**", "").replace("__", "")
    return cell.strip()


def split_row(line: str) -> list[str]:
    # Split on pipes that are not escaped with a backslash.
    parts = re.split(r"(?<!\\)\|", line.strip())
    if parts and parts[0].strip() == "":
        parts = parts[1:]
    if parts and parts[-1].strip() == "":
        parts = parts[:-1]
    return [clean_cell(p) for p in parts]


def is_separator_row(line: str) -> bool:
    cells = split_row(line)
    return bool(cells) and all(re.match(r"^:?-{3,}:?$", c) for c in cells)


def parse_code_line(line: str) -> tuple[str, str] | None:
    """Return (command, description) for one code-block line, or None."""
    stripped = line.strip()
    if not stripped or stripped.startswith("#"):
        return None
    if stripped[0] in ("'", ">", "<", ")", "`", "-"):
        return None
    first = stripped.split(None, 1)[0].lower()
    if first in SHELL_KEYWORDS:
        return None
    if stripped.startswith(("$", "http://", "https://")):
        return None
    if re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", stripped):
        return None
    if "=" in stripped.split(None, 1)[0]:
        return None
    m = re.match(r"^(.*?)\s+#\s+(.*)$", stripped)
    if m:
        return m.group(1).strip(), m.group(2).strip()
    if "=" in stripped and not stripped.startswith(("git", "docker", "ls", "cd", "cp", "mv", "rm", "mkdir", "find", "grep", "chmod", "chown", "tar", "curl", "wget", "ssh", "systemctl", "ps", "top", "kill", "uname", "df", "du", "free", "cat", "less", "head", "tail", "sort", "uniq", "awk", "sed", "wc", "touch", "nano", "vim", "ping", "ip", "ss", "netstat", "traceroute", "nslookup", "dig", "host", "apt", "pacman", "dnf", "yum", "brew", "choco", "winget", "pwsh", "powershell")):
        return None
    return stripped, ""


def parse_file(path: Path, category: str) -> list[dict]:
    text = path.read_text(encoding="utf-8")
    lines = text.splitlines()
    entries: list[dict] = []
    section = ""
    in_code = False
    code_lang = ""
    i = 0
    while i < len(lines):
        line = lines[i]
        fence = re.match(r"^```(\w*)\s*$", line.strip())
        if fence:
            if not in_code:
                in_code = True
                code_lang = fence.group(1).lower()
            else:
                in_code = False
                code_lang = ""
            i += 1
            continue
        if in_code:
            if code_lang in CODE_BLOCK_LANGS and section.lower() not in SKIP_SECTIONS:
                parsed = parse_code_line(line)
                if parsed:
                    command, description = parsed
                    entries.append({
                        "category": category,
                        "command": command,
                        "title": command,
                        "description": description,
                        "tags": f"{category},{slugify(section)}" if section else category,
                        "example": "",
                    })
            i += 1
            continue
        heading = re.match(r"^##\s+(.*)$", line)
        if heading:
            section = clean_section(heading.group(1))
            i += 1
            continue
        if line.strip().startswith("|") and section.lower() not in SKIP_SECTIONS:
            if is_separator_row(line):
                i += 1
                continue
            # Header row: the next line is a separator row.
            if i + 1 < len(lines) and is_separator_row(lines[i + 1]):
                i += 1
                continue
            cells = split_row(line)
            if len(cells) >= 2 and cells[0] and not cells[0].lower().startswith("http"):
                command = cells[0]
                description = " | ".join(c for c in cells[1:] if c)
                entries.append({
                    "category": category,
                    "command": command,
                    "title": command,
                    "description": description,
                    "tags": f"{category},{slugify(section)}" if section else category,
                    "example": "",
                })
        i += 1
    return entries


def main(argv: list[str]) -> int:
    out = REPO_ROOT / "data" / "seed.json"
    sources = list(DEFAULT_SOURCES)
    positional = [a for a in argv if not a.startswith("--")]
    if "--out" in argv:
        out = Path(argv[argv.index("--out") + 1])
    if len(positional) == 4:
        sources = [
            ("linux", "Linux", Path(positional[0])),
            ("powershell", "PowerShell", Path(positional[1])),
            ("docker", "Docker", Path(positional[2])),
            ("git", "Git", Path(positional[3])),
        ]
    elif positional:
        print("usage: generate-seed.py [linux.md powershell.md docker.md git.md] [--out path]")
        return 2

    categories = [{"name": name, "title": title} for name, title, _ in sources]
    entries: list[dict] = []
    seen: set[tuple[str, str]] = set()
    stats: dict[str, int] = {}
    for name, _title, path in sources:
        if not path.exists():
            print(f"missing source file: {path}")
            return 1
        raw = parse_file(path, name)
        kept = 0
        for e in raw:
            key = (e["category"], e["command"])
            if key in seen:
                continue
            seen.add(key)
            entries.append(e)
            kept += 1
        stats[name] = kept
        print(f"{name}: {len(raw)} parsed, {kept} kept")

    for e in entries:
        e["level"] = level_of(e["command"], e["category"])
    from collections import Counter

    dist = Counter((e["category"], e["level"]) for e in entries)
    for cat in ("linux", "powershell", "docker", "git"):
        print(f"{cat} levels: essential={dist[(cat, 1)]} common={dist[(cat, 2)]} advanced={dist[(cat, 3)]}")

    payload = {
        "version": 1,
        "generated_from": [str(p) for _, _, p in sources],
        "categories": categories,
        "entries": entries,
    }
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {out}: {len(entries)} entries in {len(categories)} categories")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
