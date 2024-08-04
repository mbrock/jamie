package aiderdoc

import (
	"time"
)

type Article struct {
	StartTime time.Time
	NewDay    bool
	Sections  []Section
}

type Section struct {
	StartTime time.Time
	Entries   []Entry
}

func ProcessEntries(entries []Entry) []Article {
	var articles []Article
	var currentArticle *Article
	var currentSection *Section
	var lastDay time.Time

	for _, entry := range entries {
		if currentArticle == nil || entry.Timestamp.Sub(currentArticle.StartTime) > time.Hour {
			// Start a new article
			newDay := !lastDay.Equal(entry.Timestamp.Truncate(24 * time.Hour))
			articles = append(articles, Article{StartTime: entry.Timestamp, NewDay: newDay})
			currentArticle = &articles[len(articles)-1]
			currentSection = nil
			lastDay = entry.Timestamp.Truncate(24 * time.Hour)
		}

		if currentSection == nil || entry.Timestamp.Sub(currentSection.StartTime) > 10*time.Minute {
			// Start a new section
			currentArticle.Sections = append(currentArticle.Sections, Section{StartTime: entry.Timestamp})
			currentSection = &currentArticle.Sections[len(currentArticle.Sections)-1]
		}

		currentSection.Entries = append(currentSection.Entries, entry)
	}

	return articles
}
