package main

import (
	"fmt"
	"os"

	"github.com/manifoldco/promptui"
)

func selectFolder(matches []TryFolder, query string) *TryFolder {
	if len(matches) == 1 {
		return &matches[0]
	}
	return showSelector(matches, query)
}

func showSelector(matches []TryFolder, query string) *TryFolder {
	if len(matches) > 3 {
		matches = matches[:3]
	}

	type option struct {
		label  string
		folder *TryFolder
	}
	options := make([]option, 0, len(matches)+1)
	for i := range matches {
		folder := &matches[i]
		options = append(options, option{label: fmt.Sprintf("%s (%s, opened %d times)", folder.Name, folder.Date, folder.TimesOpened), folder: folder})
	}
	options = append(options, option{label: fmt.Sprintf("Create new: %s", query)})

	labels := make([]string, len(options))
	for i, option := range options {
		labels[i] = option.label
	}

	if !isTerminal(os.Stdin) {
		fmt.Fprintf(os.Stderr, "Multiple matches found for %q:\n", query)
		for i, option := range options {
			marker := " "
			if i == 0 {
				marker = "→"
			}
			fmt.Fprintf(os.Stderr, "  %s %s\n", marker, option.label)
		}
		fmt.Fprintln(os.Stderr, "Using first match (run directly, not via shell function, for interactive selection)")
		return options[0].folder
	}

	prompt := promptui.Select{
		Label:  fmt.Sprintf("Multiple matches found for %q. Select one", query),
		Items:  labels,
		Size:   len(options),
		Stdin:  os.Stdin,
		Stdout: os.Stderr,
	}
	index, _, err := prompt.Run()
	if err != nil {
		os.Exit(1)
	}
	return options[index].folder
}

func printFolderInfo(folder TryFolder) {
	const gray = "\033[90m"
	const reset = "\033[0m"
	const checkmark = "✓"
	fmt.Fprintf(os.Stderr, "%s%s %s (%s, opened %d times)%s\n", gray, checkmark, folder.Name, folder.Date, folder.TimesOpened, reset)
}

func printUsage(output *os.File) {
	fmt.Fprintln(output, `Usage:
  try <name>          Create or jump to a try folder
  try list [query]    List recent folders, optionally filtered
  try prune           Remove database entries for deleted folders
  try config          Edit config interactively
  try config show     Show config file location and active values
  try init            Print shell integration
  try help            Show this help

Environment:
  TRY_DIR             Override the base folder (default: ~/try)
  TRY_PRUNE_ON_USE    Override automatic pruning (true/false)`)
}

func printConfigUsage(output *os.File) {
	fmt.Fprintln(output, `Usage:
  try config          Edit config interactively
  try config show     Show config file location and active values`)
}

func printConfigInfo(config Config) {
	path, err := configPath()
	if err != nil {
		path = configFile
	}
	fmt.Fprintf(os.Stderr, `Config file: %s

Current config:
  try_dir = %q
  prune_on_use = %t

Example config:
  try_dir = "~/try"
  prune_on_use = true
`, path, config.TryDir, config.PruneOnUse)
}

func printShellIntegration() {
	fmt.Println(`# Try shell integration
# Add this to your ~/.bashrc or ~/.zshrc:

try() {
    local output
    # Only capture stdout for cd command, let stderr through for interactive prompts
    output=$(command try "$@" 2>/dev/tty)
    if [ $? -eq 0 ]; then
        eval "$output"
    else
        return 1
    fi
}`)
}
