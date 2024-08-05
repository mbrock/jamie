package aiderdoc

import (
	"time"
)

type Article struct {
	StartTime time.Time
	NewDay    bool
	Entries   []Entry
}

func ProcessEntries(entries []Entry) []Article {
	var articles []Article
	var currentArticle *Article
	var lastDay time.Time

	for _, entry := range entries {
		if currentArticle == nil ||
			entry.Timestamp.Sub(currentArticle.StartTime) > time.Hour {
			// Start a new article
			newDay := !lastDay.Equal(entry.Timestamp.Truncate(24 * time.Hour))
			articles = append(
				articles,
				Article{StartTime: entry.Timestamp, NewDay: newDay},
			)
			currentArticle = &articles[len(articles)-1]
			lastDay = entry.Timestamp.Truncate(24 * time.Hour)
		}

		if entry.Type != EntryTypeClear {
			currentArticle.Entries = append(currentArticle.Entries, entry)
		}
	}

	return articles
}
