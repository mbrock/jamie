package aiderdoc

import (
	"time"
)

type Article struct {
	StartTime time.Time
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

	for _, entry := range entries {
		if currentArticle == nil || entry.Timestamp.Sub(currentArticle.StartTime) > time.Hour {
			// Start a new article
			articles = append(articles, Article{StartTime: entry.Timestamp})
			currentArticle = &articles[len(articles)-1]
			currentSection = nil
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
