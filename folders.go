package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/sahilm/fuzzy"
)

func getAllFolders(db *sqlDB) ([]TryFolder, error) {
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
		var folder TryFolder
		var createdAt, lastOpened string
		if err := rows.Scan(&folder.ID, &folder.Path, &folder.Name, &folder.Date, &createdAt, &folder.TimesOpened, &lastOpened); err != nil {
			return nil, err
		}
		folder.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		folder.LastOpened, _ = time.Parse(time.RFC3339, lastOpened)
		folders = append(folders, folder)
	}
	return folders, rows.Err()
}

func fuzzySearch(query string, folders []TryFolder) []TryFolder {
	if query == "" {
		return folders
	}

	names := make([]string, len(folders))
	for i, folder := range folders {
		names[i] = folder.Name
	}

	type scoredMatch struct {
		folder TryFolder
		score  int
	}
	matches := fuzzy.Find(query, names)
	scored := make([]scoredMatch, len(matches))
	for i, match := range matches {
		scored[i] = scoredMatch{folder: folders[match.Index], score: match.Score}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].folder.TimesOpened != scored[j].folder.TimesOpened {
			return scored[i].folder.TimesOpened > scored[j].folder.TimesOpened
		}
		return scored[i].folder.LastOpened.After(scored[j].folder.LastOpened)
	})

	result := make([]TryFolder, len(scored))
	for i, match := range scored {
		result[i] = match.folder
	}
	return result
}

func addFolder(db *sqlDB, folder TryFolder) error {
	_, err := db.Exec(`
		INSERT INTO folders (path, name, date, created_at, times_opened, last_opened)
		VALUES (?, ?, ?, ?, ?, ?)
	`, folder.Path, folder.Name, folder.Date, folder.CreatedAt.Format(time.RFC3339), folder.TimesOpened, folder.LastOpened.Format(time.RFC3339))
	return err
}

func updateFolder(db *sqlDB, folder TryFolder) error {
	_, err := db.Exec(`UPDATE folders SET times_opened = ?, last_opened = ? WHERE id = ?`, folder.TimesOpened, folder.LastOpened.Format(time.RFC3339), folder.ID)
	return err
}

func deleteFolderRecord(db *sqlDB, id int) error {
	_, err := db.Exec(`DELETE FROM folders WHERE id = ?`, id)
	return err
}

func listFolders(db *sqlDB, query string) error {
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

func pruneMissingFolders(db *sqlDB, verbose bool) error {
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

	if verbose {
		if removed == 0 {
			fmt.Fprintln(os.Stderr, "No missing try folders found.")
		} else {
			fmt.Fprintf(os.Stderr, "Pruned %d missing try folder(s).\n", removed)
		}
	}
	return nil
}
