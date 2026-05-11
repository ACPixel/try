package main

import "time"

const (
	tryDir               = "~/try"
	configFile           = "~/.config/try/config"
	dbFileName           = "try.db"
	currentSchemaVersion = 1
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

type Config struct {
	TryDir     string
	PruneOnUse bool
}
