# try

A Go implementation of the Ruby `try` utility for quickly creating and navigating to temporary project folders.

**Note**: A child process cannot change the parent shell's directory, so shell integration is required. This is the same approach used by tools like `zoxide`, `autojump`, and `z`.

## Features

- Create dated folders: `try some name` creates `~/try/2025-10-09-some-name`
- Fuzzy search: Automatically finds and navigates to existing folders based on name (using [sahilm/fuzzy](https://github.com/sahilm/fuzzy))
- Interactive selector: When multiple matches are found, shows an interactive menu with top 3 matches and "create new" option
- Informative output: Displays folder name, creation date, and usage count when navigating
- Tracks usage: SQLite database tracks folder creation dates, open count, and last opened time
- Smart sorting: Results sorted by fuzzy match score, usage frequency, and last opened time

## Installation

Install using Go:
```bash
go install github.com/ACPixel/try@latest
```

Or clone and build locally:
```bash
git clone https://github.com/ACPixel/try.git
cd try
go install
```

## Setup Shell Integration

After installing, run:
```bash
try init
```

This will output shell integration code. Add it to your `~/.bashrc` or `~/.zshrc`:
```bash
try init >> ~/.bashrc
source ~/.bashrc
```

Or manually add:
```bash
try() {
    local output
    # Only capture stdout for cd command, let stderr through for interactive prompts
    output=$(command try "$@" 2>/dev/tty)
    if [ $? -eq 0 ]; then
        eval "$output"
    else
        return 1
    fi
}
```

## Usage

After adding the alias, simply run:
```bash
# Create a new folder and cd into it
try my-project
# Creates: ~/try/2024-01-15-my-project and changes directory

# Later, fuzzy search will find it and cd into it
try my-proj
# Navigates to: ~/try/2024-01-15-my-project
# Output: ✓ my-project (2024-01-15, opened 2 times)

# If multiple matches are found, an interactive selector appears
try project
# Shows interactive menu with top 3 matches:
#   > project-a (2024-01-10, opened 5 times)
#     project-b (2024-01-12, opened 3 times)
#     project-c (2024-01-14, opened 1 times)
#     Create new: project
```

## How it works

1. The Go binary searches the database for existing folders matching your query using fuzzy search
2. If a single match is found, it displays folder info, updates usage stats, and outputs `cd "path"`
3. If multiple matches are found, it shows an interactive selector with the top 3 matches and a "create new" option
4. If no match is found, it creates a new dated folder and outputs `cd "path"`
5. The shell function wraps the binary and `eval`s the output to change directory
6. Folder information (name, date, usage count) is displayed in gray with a checkmark for easy identification

## Installing from Source

```bash
# Clone the repository
git clone https://github.com/ACPixel/try.git
cd try

# Install to $GOPATH/bin (or $HOME/go/bin if GOPATH is not set)
go install

# Or build locally
go build -o try main.go
```

## Database

The SQLite database is stored at `~/try/try.db` and tracks:
- Folder paths
- Names
- Creation dates
- Times opened
- Last opened timestamp

