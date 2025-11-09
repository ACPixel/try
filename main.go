package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	_ "github.com/mattn/go-sqlite3"
	"github.com/sahilm/fuzzy"
)

const (
	tryDir     = "~/try"
	dbFileName = "try.db"
)

type TryFolder struct {
	ID          int
	Path        string
	Name        string
	Date        string
	CreatedAt   time.Time
	TimesOpened int
	LastOpened  time.Time
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: try <name>\n")
		fmt.Fprintf(os.Stderr, "       try init   (show shell integration)\n")
		os.Exit(1)
	}

	// Handle init command to show shell integration
	if os.Args[1] == "init" {
		printShellIntegration()
		return
	}

	name := strings.Join(os.Args[1:], " ")

	tryBaseDir, err := expandHomeDir(tryDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error expanding home directory: %v\n", err)
		os.Exit(1)
	}

	// Ensure try directory exists
	if err := os.MkdirAll(tryBaseDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating try directory: %v\n", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(tryBaseDir, dbFileName)
	db, err := initDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Check if folder with this name already exists (fuzzy search)
	folders, err := getAllFolders(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading folders: %v\n", err)
		os.Exit(1)
	}

	if len(folders) > 0 {
		// Perform fuzzy search
		matches := fuzzySearch(name, folders)

		if len(matches) > 0 {
			var selectedFolder *TryFolder

			// If there are multiple matches, show selector
			if len(matches) > 1 {
				selectedFolder = showSelector(matches, name)
				if selectedFolder == nil {
					// User selected "create new" or cancelled
					// Fall through to create new folder
				} else {
					// Update times opened
					selectedFolder.TimesOpened++
					selectedFolder.LastOpened = time.Now()
					if err := updateFolder(db, *selectedFolder); err != nil {
						fmt.Fprintf(os.Stderr, "Error updating folder: %v\n", err)
						os.Exit(1)
					}

					// Note: promptui already displays the selected item, so we don't need to print it again

					// Output cd command for shell to eval
					fmt.Printf("cd %q\n", selectedFolder.Path)
					return
				}
			} else {
				// Single match, use it directly
				bestMatch := matches[0]

				// Update times opened
				bestMatch.TimesOpened++
				bestMatch.LastOpened = time.Now()
				if err := updateFolder(db, bestMatch); err != nil {
					fmt.Fprintf(os.Stderr, "Error updating folder: %v\n", err)
					os.Exit(1)
				}

				// Show folder info
				printFolderInfo(bestMatch)

				// Output cd command for shell to eval
				fmt.Printf("cd %q\n", bestMatch.Path)
				return
			}
		}
	}

	// Create new folder
	today := time.Now().Format("2006-01-02")
	folderName := fmt.Sprintf("%s-%s", today, name)
	folderPath := filepath.Join(tryBaseDir, folderName)

	if err := os.MkdirAll(folderPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating folder: %v\n", err)
		os.Exit(1)
	}

	// Add to database
	folder := TryFolder{
		Path:        folderPath,
		Name:        name,
		Date:        today,
		CreatedAt:   time.Now(),
		TimesOpened: 1,
		LastOpened:  time.Now(),
	}

	if err := addFolder(db, folder); err != nil {
		fmt.Fprintf(os.Stderr, "Error adding folder to database: %v\n", err)
		os.Exit(1)
	}

	// Output cd command for shell to eval
	fmt.Printf("cd %q\n", folderPath)
}

func expandHomeDir(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return strings.Replace(path, "~", home, 1), nil
	}
	return path, nil
}

func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS folders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		date TEXT NOT NULL,
		created_at TEXT NOT NULL,
		times_opened INTEGER DEFAULT 1,
		last_opened TEXT NOT NULL
	);
	`

	if _, err := db.Exec(createTableSQL); err != nil {
		return nil, err
	}

	return db, nil
}

func getAllFolders(db *sql.DB) ([]TryFolder, error) {
	rows, err := db.Query(`
		SELECT id, path, name, date, created_at, times_opened, last_opened
		FROM folders
		ORDER BY last_opened DESC, times_opened DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []TryFolder
	for rows.Next() {
		var f TryFolder
		var createdAtStr, lastOpenedStr string
		err := rows.Scan(&f.ID, &f.Path, &f.Name, &f.Date, &createdAtStr, &f.TimesOpened, &lastOpenedStr)
		if err != nil {
			return nil, err
		}

		f.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		f.LastOpened, _ = time.Parse(time.RFC3339, lastOpenedStr)

		folders = append(folders, f)
	}

	return folders, rows.Err()
}

func fuzzySearch(query string, folders []TryFolder) []TryFolder {
	if query == "" {
		return folders
	}

	// Create a slice of strings for fuzzy matching
	folderList := make([]string, len(folders))
	for i, folder := range folders {
		folderList[i] = folder.Name
	}

	// Use fuzzy library to find matches
	matches := fuzzy.Find(query, folderList)

	if len(matches) == 0 {
		return []TryFolder{}
	}

	// Create result slice with matched folders
	type scoredMatch struct {
		folder TryFolder
		score  int
	}

	scoredMatches := make([]scoredMatch, len(matches))
	for i, match := range matches {
		scoredMatches[i] = scoredMatch{
			folder: folders[match.Index],
			score:  match.Score,
		}
	}

	// Sort by fuzzy score (higher is better), then by times opened, then by last opened
	sort.Slice(scoredMatches, func(i, j int) bool {
		if scoredMatches[i].score != scoredMatches[j].score {
			return scoredMatches[i].score > scoredMatches[j].score
		}
		if scoredMatches[i].folder.TimesOpened != scoredMatches[j].folder.TimesOpened {
			return scoredMatches[i].folder.TimesOpened > scoredMatches[j].folder.TimesOpened
		}
		return scoredMatches[i].folder.LastOpened.After(scoredMatches[j].folder.LastOpened)
	})

	result := make([]TryFolder, len(scoredMatches))
	for i, m := range scoredMatches {
		result[i] = m.folder
	}

	return result
}

func addFolder(db *sql.DB, folder TryFolder) error {
	_, err := db.Exec(`
		INSERT INTO folders (path, name, date, created_at, times_opened, last_opened)
		VALUES (?, ?, ?, ?, ?, ?)
	`, folder.Path, folder.Name, folder.Date, folder.CreatedAt.Format(time.RFC3339), folder.TimesOpened, folder.LastOpened.Format(time.RFC3339))
	return err
}

func updateFolder(db *sql.DB, folder TryFolder) error {
	_, err := db.Exec(`
		UPDATE folders
		SET times_opened = ?, last_opened = ?
		WHERE id = ?
	`, folder.TimesOpened, folder.LastOpened.Format(time.RFC3339), folder.ID)
	return err
}

func showSelector(matches []TryFolder, query string) *TryFolder {
	// Limit to top 3 matches
	maxMatches := 3
	if len(matches) > maxMatches {
		matches = matches[:maxMatches]
	}

	// Create options for the selector
	type option struct {
		label  string
		folder *TryFolder
	}

	options := make([]option, 0, len(matches)+1)

	// Add folder options
	for i := range matches {
		folder := &matches[i]
		label := fmt.Sprintf("%s (%s, opened %d times)", folder.Name, folder.Date, folder.TimesOpened)
		options = append(options, option{label: label, folder: folder})
	}

	// Add "create new" option
	options = append(options, option{label: fmt.Sprintf("Create new: %s", query), folder: nil})

	// Create labels slice for promptui
	labels := make([]string, len(options))
	for i, opt := range options {
		labels[i] = opt.label
	}

	// Check if we have an interactive terminal
	// If stdin is not a TTY, we can't show an interactive prompt
	// In that case, fall back to non-interactive mode
	if !isTerminal(os.Stdin) {
		// Non-interactive: print options to stderr and use first match
		fmt.Fprintf(os.Stderr, "Multiple matches found for '%s':\n", query)
		for i, opt := range options {
			marker := " "
			if i == 0 {
				marker = "→"
			}
			fmt.Fprintf(os.Stderr, "  %s %s\n", marker, opt.label)
		}
		fmt.Fprintf(os.Stderr, "Using first match (run directly, not via shell function, for interactive selection)\n")
		// Return first match (or nil if "create new" is first, which shouldn't happen)
		if options[0].folder != nil {
			return options[0].folder
		}
		return nil
	}

	// Create promptui selector with explicit stderr output
	// This ensures the prompt displays even when stdout is captured
	prompt := promptui.Select{
		Label:  fmt.Sprintf("Multiple matches found for '%s'. Select one:", query),
		Items:  labels,
		Size:   len(options),
		Stdin:  os.Stdin,
		Stdout: os.Stderr, // Write prompt to stderr so it's not captured
	}

	index, _, err := prompt.Run()
	if err != nil {
		// User cancelled (Ctrl+C)
		os.Exit(1)
	}

	return options[index].folder
}

// printFolderInfo displays folder information to stderr
func printFolderInfo(folder TryFolder) {
	// ANSI color codes: gray text and reset
	const gray = "\033[90m"
	const reset = "\033[0m"
	const checkmark = "✓"
	fmt.Fprintf(os.Stderr, "%s%s %s (%s, opened %d times)%s\n", gray, checkmark, folder.Name, folder.Date, folder.TimesOpened, reset)
}

// isTerminal checks if the given file is a terminal
func isTerminal(f *os.File) bool {
	fileInfo, err := f.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
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
