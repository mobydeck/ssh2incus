package pkg

import (
	"fmt"
	"strings"
	"time"
)

type AppConfig struct {
	Name    string
	Version string
	Edition string
	GitHash string
	BuiltAt string
}

type App struct {
	name     string
	version  string
	edition  string
	gitHash  string
	longName string
	builtAt  string

	startTime time.Time
}

func NewApp(c AppConfig) *App {
	a := &App{
		name:      c.Name,
		version:   c.Version,
		edition:   c.Edition,
		gitHash:   c.GitHash,
		builtAt:   c.BuiltAt,
		startTime: time.Now(),
	}
	return a
}

func (a *App) Name() string {
	return a.name
}

func (a *App) NAME() string {
	return strings.ToUpper(a.name)
}

func (a *App) LongName() string {
	name := a.String()
	if a.gitHash != "" {
		name += fmt.Sprintf(" (%s)", a.gitHash)
	}
	return name
}

func (a *App) Version() string {
	return a.version
}

func (a *App) GitHash() string {
	return a.gitHash
}

func (a *App) Commit() string {
	return a.gitHash
}

func (a *App) String() string {
	return a.name + " " + a.version
}

func (a *App) BuiltAt() string {
	return a.builtAt
}

func (a *App) Uptime() string {
	// Calculate the duration since the process started
	uptime := time.Since(a.startTime)

	// Format the uptime in a human-readable format
	return formatDuration(uptime)
}

// formatDuration converts a time.Duration to a friendly string format
func formatDuration(d time.Duration) string {
	// Round to seconds
	d = d.Round(time.Second)

	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	// Build the string representation based on the duration
	parts := []string{}

	if days > 0 {
		if days == 1 {
			parts = append(parts, "1 day")
		} else {
			parts = append(parts, fmt.Sprintf("%d days", days))
		}
	}

	if hours > 0 {
		if hours == 1 {
			parts = append(parts, "1 hour")
		} else {
			parts = append(parts, fmt.Sprintf("%d hours", hours))
		}
	}

	if minutes > 0 {
		if minutes == 1 {
			parts = append(parts, "1 minute")
		} else {
			parts = append(parts, fmt.Sprintf("%d minutes", minutes))
		}
	}

	if seconds > 0 || len(parts) == 0 {
		if seconds == 1 {
			parts = append(parts, "1 second")
		} else {
			parts = append(parts, fmt.Sprintf("%d seconds", seconds))
		}
	}

	return strings.Join(parts, ", ")
}
