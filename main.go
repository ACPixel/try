package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
		fatal("Error reading config: %v", err)
	}

	if command == "config" {
		handleConfigCommand(config, os.Args[2:])
		return
	}

	db, err := openAppDB(config)
	if err != nil {
		fatal("Error initializing database: %v", err)
	}
	defer db.Close()

	if config.PruneOnUse && command != "prune" && command != "clean" {
		if err := pruneMissingFolders(db, false); err != nil {
			fatal("Error pruning folders: %v", err)
		}
	}

	switch command {
	case "list", "ls":
		if err := listFolders(db, strings.Join(os.Args[2:], " ")); err != nil {
			fatal("Error listing folders: %v", err)
		}
	case "prune", "clean":
		if err := pruneMissingFolders(db, true); err != nil {
			fatal("Error pruning folders: %v", err)
		}
	default:
		if err := openOrCreateTry(db, config, strings.Join(os.Args[1:], " ")); err != nil {
			fatal("%v", err)
		}
	}
}

func openAppDB(config Config) (*sqlDB, error) {
	tryBaseDir, err := getTryBaseDir(config)
	if err != nil {
		return nil, fmt.Errorf("expanding try directory: %w", err)
	}
	if err := os.MkdirAll(tryBaseDir, 0755); err != nil {
		return nil, fmt.Errorf("creating try directory: %w", err)
	}
	return initDB(filepath.Join(tryBaseDir, dbFileName))
}

func openOrCreateTry(db *sqlDB, config Config, name string) error {
	folders, err := getAllFolders(db)
	if err != nil {
		return fmt.Errorf("Error reading folders: %w", err)
	}

	if matches := fuzzySearch(name, folders); len(matches) > 0 {
		folder := selectFolder(matches, name)
		if folder != nil {
			folder.TimesOpened++
			folder.LastOpened = time.Now()
			if err := updateFolder(db, *folder); err != nil {
				return fmt.Errorf("Error updating folder: %w", err)
			}
			if len(matches) == 1 {
				printFolderInfo(*folder)
			}
			fmt.Printf("cd %q\n", folder.Path)
			return nil
		}
	}

	return createTryFolder(db, config, name)
}

func createTryFolder(db *sqlDB, config Config, name string) error {
	tryBaseDir, err := getTryBaseDir(config)
	if err != nil {
		return fmt.Errorf("Error expanding home directory: %w", err)
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	folderPath := filepath.Join(tryBaseDir, fmt.Sprintf("%s-%s", today, slugifyName(name)))
	if err := os.MkdirAll(folderPath, 0755); err != nil {
		return fmt.Errorf("Error creating folder: %w", err)
	}

	folder := TryFolder{Path: folderPath, Name: name, Date: today, CreatedAt: now, TimesOpened: 1, LastOpened: now}
	if err := addFolder(db, folder); err != nil {
		return fmt.Errorf("Error adding folder to database: %w", err)
	}

	fmt.Printf("cd %q\n", folderPath)
	return nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
