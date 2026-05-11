package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/manifoldco/promptui"
)

func defaultConfig() Config {
	return Config{TryDir: tryDir, PruneOnUse: true}
}

func loadConfig() (Config, error) {
	config := defaultConfig()
	path, err := configPath()
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

func handleConfigCommand(config Config, args []string) {
	if len(args) > 0 && args[0] == "show" {
		printConfigInfo(config)
		return
	}
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		printConfigUsage(os.Stderr)
		return
	}
	if !isTerminal(os.Stdin) {
		printConfigInfo(config)
		return
	}
	if err := editConfigInteractive(config); err != nil {
		fatal("Error updating config: %v", err)
	}
}

func editConfigInteractive(config Config) error {
	for {
		items := []string{
			fmt.Sprintf("try_dir: %s", config.TryDir),
			fmt.Sprintf("prune_on_use: %t", config.PruneOnUse),
			"Save and exit",
			"Cancel",
		}
		prompt := promptui.Select{Label: "Configure try", Items: items, Size: len(items), Stdin: os.Stdin, Stdout: os.Stderr}
		index, _, err := prompt.Run()
		if err != nil {
			return err
		}

		switch index {
		case 0:
			value, err := promptString("try_dir", config.TryDir)
			if err != nil {
				return err
			}
			if strings.TrimSpace(value) != "" {
				config.TryDir = strings.TrimSpace(value)
			}
		case 1:
			config.PruneOnUse = !config.PruneOnUse
		case 2:
			if err := saveConfig(config); err != nil {
				return err
			}
			path, _ := configPath()
			fmt.Fprintf(os.Stderr, "Saved config to %s\n", path)
			return nil
		case 3:
			fmt.Fprintln(os.Stderr, "Config unchanged.")
			return nil
		}
	}
}

func promptString(label, current string) (string, error) {
	prompt := promptui.Prompt{Label: label, Default: current, Stdin: os.Stdin, Stdout: os.Stderr}
	return prompt.Run()
}

func saveConfig(config Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	contents := fmt.Sprintf("try_dir = %q\nprune_on_use = %t\n", config.TryDir, config.PruneOnUse)
	return os.WriteFile(path, []byte(contents), 0644)
}

func configPath() (string, error) {
	return expandHomeDir(configFile)
}

func getTryBaseDir(config Config) (string, error) {
	return expandHomeDir(config.TryDir)
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
