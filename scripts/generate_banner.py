import argparse
import os

def colored(r, g, b, text):
    return f"\033[38;2;{r};{g};{b}m{text}\033[0m"

# HOTPLEX ASCII Art (refining the block style)
# Each letter is 6 lines high.

h = [
    "██╗  ██╗",
    "██║  ██║",
    "███████║",
    "██╔══██║",
    "██║  ██║",
    "╚═╝  ╚═╝"
]

o = [
    " ██████╗ ",
    "██╔═══██╗",
    "██║   ██║",
    "██║   ██║",
    "╚██████╔╝",
    " ╚═════╝ "
]

t = [
    "████████╗",
    "╚══██╔══╝",
    "   ██║   ",
    "   ██║   ",
    "   ██║   ",
    "   ╚═╝   "
]

p = [
    "██████╗ ",
    "██╔══██╗",
    "██████╔╝",
    "██╔═══╝ ",
    "██║     ",
    "╚═╝     "
]

l = [
    "██╗     ",
    "██║     ",
    "██║     ",
    "██║     ",
    "███████╗",
    "╚══════╝"
]

e = [
    "███████╗",
    "██╔════╝",
    "█████╗  ",
    "██╔══╝  ",
    "███████╗",
    "╚══════╝"
]

x = [
    "██╗  ██╗",
    "╚██╗██╔╝",
    " ╚███╔╝ ",
    " ██╔██╗ ",
    "██╔╝ ██╗",
    "╚═╝  ╚═╝"
]

# Colors:
# Hot (Orange): 255, 138, 0
# Plex (Cyan): 0, 185, 203

def get_line(i):
    # HOT (Orange)
    line = colored(255, 138, 0, h[i] + o[i] + t[i])
    # Spacer
    line += "  "
    # PLEX (Cyan)
    line += colored(0, 185, 203, p[i] + l[i] + e[i] + x[i])
    return line

banner = []
for i in range(6):
    banner.append(get_line(i))

# Add a subtitle/tagline without nodes and version
parser = argparse.ArgumentParser(description="Generate HOTPLEX banner_art.txt")
parser.add_argument("--pad", type=int, default=2, help="Left padding spaces (default: 2)")
parser.add_argument("--output", default=None, help="Output file path")
args = parser.parse_args()

line_sep = colored(60, 60, 60, "─────────────────────────────────────────────────────────────")
tagline = colored(255, 138, 0, "HOTPLEX ") + colored(0, 185, 203, "GATEWAY")
desc = colored(100, 100, 100, "Unified AI Coding Agent Access Layer")

banner.append("")
banner.append(line_sep)
banner.append(tagline)
banner.append(desc)
banner.append(line_sep)

# Apply left padding to all lines.
prefix = " " * args.pad
banner = [prefix + line for line in banner]

output = "\n".join(banner)
out_path = args.output or os.path.join(os.path.dirname(__file__), "..", "cmd", "hotplex", "banner_art.txt")
with open(out_path, "w") as f:
    f.write(output)

print(f"Banner generated with {args.pad}-space left padding → {out_path}")
