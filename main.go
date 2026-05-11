package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	_ "github.com/mattn/go-sqlite3"
	"github.com/sahilm/fuzzy"
)

const (
	tryDir               = "~/try"
	configFile           = "~/.config/try/config"
	dbFileName           = "try.db"
	currentSchemaVersion = 1
)

var nonSlugChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type TryFolder struct {
	ID          int
	Path        string
	Name        string
	Date        string
	CreatedAt   time.Time
	TimesOpened int
	LastOpened  time.Time
}

type Config struct {
	TryDir     string
	PruneOnUse bool
}

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "help", "--help", "-h":
		printUsage(os.Stderr)
		return
	case "init":
		printShellIntegration()
		return
	}

	config, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}

	tryBaseDir, err := getTryBaseDir(config)
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

	if config.PruneOnUse && command != "prune" && command != "clean" {
		if err := pruneMissingFolders(db, false); err != nil {
			fmt.Fprintf(os.Stderr, "Error pruning folders: %v\n", err)
			os.Exit(1)
		}
	}

	switch command {
	case "list", "ls":
		query := strings.Join(os.Args[2:], " ")
		if err := listFolders(db, query); err != nil {
			fmt.Fprintf(os.Stderr, "Error listing folders: %v\n", err)
			os.Exit(1)
		}
		return
	case "config":
		printConfigInfo(config)
		return
	case "prune", "clean":
		if err := pruneMissingFolders(db, true); err != nil {
			fmt.Fprintf(os.Stderr, "Error pruning folders: %v\n", err)
			os.Exit(1)
		}
		return
	}

	name := strings.Join(os.Args[1:], " ")

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
	folderName := fmt.Sprintf("%s-%s", today, slugifyName(name))
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

func defaultConfig() Config {
	return Config{
		TryDir:     tryDir,
		PruneOnUse: true,
	}
}

func loadConfig() (Config, error) {
	config := defaultConfig()
	path, err := expandHomeDir(configFile)
	if err != nil {
		return config, err
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, applyEnvOverrides(&config)
		}
		return config, err
	}

	for lineNumber, rawLine := range strings.Split(string(contents), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return config, fmt.Errorf("%s:%d: expected key = value", path, lineNumber+1)
		}

		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)

		switch key {
		case "try_dir":
			if value != "" {
				config.TryDir = value
			}
		case "prune_on_use":
			parsed, err := parseBool(value)
			if err != nil {
				return config, fmt.Errorf("%s:%d: %w", path, lineNumber+1, err)
			}
			config.PruneOnUse = parsed
		default:
			return config, fmt.Errorf("%s:%d: unknown config key %q", path, lineNumber+1, key)
		}
	}

	return config, applyEnvOverrides(&config)
}

func applyEnvOverrides(config *Config) error {
	if dir := os.Getenv("TRY_DIR"); dir != "" {
		config.TryDir = dir
	}
	if prune := os.Getenv("TRY_PRUNE_ON_USE"); prune != "" {
		parsed, err := parseBool(prune)
		if err != nil {
			return fmt.Errorf("TRY_PRUNE_ON_USE: %w", err)
		}
		config.PruneOnUse = parsed
	}
	return nil
}

func parseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("expected boolean, got %q", value)
	}
}

func getTryBaseDir(config Config) (string, error) {
	return expandHomeDir(config.TryDir)
}

func slugifyName(name string) string {
	slug := strings.Trim(nonSlugChars.ReplaceAllString(strings.TrimSpace(name), "-"), "-")
	if slug == "" {
		return "untitled"
	}
	return slug
}

func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	if err := migrateDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateDB(db *sql.DB) error {
	version, err := schemaVersion(db)
	if err != nil {
		return err
	}

	if version > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than this try version supports (%d)", version, currentSchemaVersion)
	}

	if version < 1 {
		if err := migrateToV1(db); err != nil {
			return err
		}
		version = 1
	}

	return nil
}

func schemaVersion(db *sql.DB) (int, error) {
	exists, err := tableExists(db, "schema_migrations")
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}

	var version int
	err = db.QueryRow(`SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return version, err
}

func tableExists(db *sql.DB, table string) (bool, error) {
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func migrateToV1(db *sql.DB) error {
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

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec(createTableSQL); err != nil {
		_ = tx.Rollback()
		return err
	}

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
	`); err != nil {
		_ = tx.Rollback()
		return err
	}

	if _, err := tx.Exec(`
		INSERT OR REPLACE INTO schema_migrations (version, applied_at)
		VALUES (?, ?);
	`, currentSchemaVersion, time.Now().Format(time.RFC3339)); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
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

func deleteFolderRecord(db *sql.DB, id int) error {
	_, err := db.Exec(`DELETE FROM folders WHERE id = ?`, id)
	return err
}

func listFolders(db *sql.DB, query string) error {
	folders, err := getAllFolders(db)
	if err != nil {
		return err
	}

	if query != "" {
		folders = fuzzySearch(query, folders)
	}

	if len(folders) == 0 {
		if query == "" {
			fmt.Fprintln(os.Stderr, "No try folders yet. Create one with: try <name>")
		} else {
			fmt.Fprintf(os.Stderr, "No try folders match %q.\n", query)
		}
		return nil
	}

	for _, folder := range folders {
		fmt.Fprintf(os.Stderr, "%s  %s  opened %d  %s\n", folder.Date, folder.Name, folder.TimesOpened, folder.Path)
	}
	return nil
}

func pruneMissingFolders(db *sql.DB, verbose bool) error {
	folders, err := getAllFolders(db)
	if err != nil {
		return err
	}

	removed := 0
	for _, folder := range folders {
		if _, err := os.Stat(folder.Path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}

		if err := deleteFolderRecord(db, folder.ID); err != nil {
			return err
		}
		removed++
		if verbose {
			fmt.Fprintf(os.Stderr, "Pruned missing folder: %s\n", folder.Path)
		}
	}

	if !verbose {
		return nil
	}

	if removed == 0 {
		fmt.Fprintln(os.Stderr, "No missing try folders found.")
	} else {
		fmt.Fprintf(os.Stderr, "Pruned %d missing try folder(s).\n", removed)
	}
	return nil
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
		Label:  fmt.Sprintf("Multiple matches found for '%s'. Select one", query),
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

func printUsage(output *os.File) {
	fmt.Fprintln(output, `Usage:
  try <name>          Create or jump to a try folder
  try list [query]    List recent folders, optionally filtered
  try prune           Remove database entries for deleted folders
  try config          Show config file location and defaults
  try init            Print shell integration
  try help            Show this help

Environment:
  TRY_DIR             Override the base folder (default: ~/try)
  TRY_PRUNE_ON_USE    Override automatic pruning (true/false)`)
}

func printConfigInfo(config Config) {
	path, err := expandHomeDir(configFile)
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
