package speechmatics

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Sentence struct {
	StartTime float64
	EndTime   float64
	Speaker   string
	Content   string
}

type Silence struct {
	StartTime float64
	EndTime   float64
}

type Page struct {
	Sentences []Sentence
	Silences  []Silence
}

type Transcript struct {
	Pages []Page
}

func ParseTranscript(jsonData []byte) (*Transcript, error) {
	var rawTranscript struct {
		Results []TranscriptResult `json:"results"`
	}

	err := json.Unmarshal(jsonData, &rawTranscript)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %v", err)
	}

	transcript := &Transcript{}
	var currentPage Page
	var currentSentence Sentence
	var lastEndTime float64

	for _, result := range rawTranscript.Results {
		if len(result.Alternatives) == 0 {
			continue
		}

		alt := result.Alternatives[0]

		if currentSentence.StartTime == 0 {
			currentSentence.StartTime = result.StartTime
		}

		if result.Type == "punctuation" {
			currentSentence.Content += alt.Content
		} else {
			if len(currentSentence.Content) > 0 && !strings.HasSuffix(currentSentence.Content, " ") {
				currentSentence.Content += " "
			}
			currentSentence.Content += alt.Content
		}

		if result.IsEOS ||
			(result.Type == "punctuation" && (alt.Content == "." || alt.Content == "!" || alt.Content == "?")) {
			currentSentence.EndTime = result.EndTime
			currentSentence.Content = strings.TrimSpace(
				currentSentence.Content,
			)

			if currentSentence.StartTime-lastEndTime > 1.0 &&
				lastEndTime != 0 {
				silence := Silence{
					StartTime: lastEndTime,
					EndTime:   currentSentence.StartTime,
				}
				currentPage.Silences = append(currentPage.Silences, silence)

				if result.EndTime-currentPage.Sentences[0].StartTime > 300.0 { // 5 minutes
					transcript.Pages = append(transcript.Pages, currentPage)
					currentPage = Page{}
				}
			}

			currentPage.Sentences = append(
				currentPage.Sentences,
				currentSentence,
			)
			lastEndTime = currentSentence.EndTime
			currentSentence = Sentence{}
		}
	}

	if len(currentSentence.Content) > 0 || len(currentPage.Sentences) == 0 {
		currentSentence.EndTime = rawTranscript.Results[len(rawTranscript.Results)-1].EndTime
		currentSentence.Content = strings.TrimSpace(currentSentence.Content)
		currentPage.Sentences = append(currentPage.Sentences, currentSentence)
	}

	if len(currentPage.Sentences) > 0 {
		transcript.Pages = append(transcript.Pages, currentPage)
	}

	// Adjust start and end times between pages
	for i := 1; i < len(transcript.Pages); i++ {
		prevPage := &transcript.Pages[i-1]
		currentPage := &transcript.Pages[i]

		gap := currentPage.Sentences[0].StartTime - prevPage.Sentences[len(prevPage.Sentences)-1].EndTime
		midPoint := prevPage.Sentences[len(prevPage.Sentences)-1].EndTime + gap/2

		prevPage.Sentences[len(prevPage.Sentences)-1].EndTime = midPoint
		currentPage.Sentences[0].StartTime = midPoint
	}

	// Ensure the last page covers until the end of the audio
	if len(transcript.Pages) > 0 {
		lastPage := &transcript.Pages[len(transcript.Pages)-1]
		lastSentence := &lastPage.Sentences[len(lastPage.Sentences)-1]
		lastSentence.EndTime = rawTranscript.Results[len(rawTranscript.Results)-1].EndTime
	}

	return transcript, nil
}

func PrintTranscript(transcript *Transcript) {
	for i, page := range transcript.Pages {
		if i > 0 {
			fmt.Println("---")
		}
		pageStartTime := page.Sentences[0].StartTime
		pageEndTime := page.Sentences[len(page.Sentences)-1].EndTime
		fmt.Printf("Page %d (%02d:%02d-%02d:%02d):\n",
			i+1,
			int(pageStartTime)/60, int(pageStartTime)%60,
			int(pageEndTime)/60, int(pageEndTime)%60)
		for j, sentence := range page.Sentences {
			if j > 0 && len(page.Silences) > j-1 {
				silence := page.Silences[j-1]
				fmt.Printf("\n[Silence: %02d:%02d-%02d:%02d]\n\n",
					int(silence.StartTime)/60, int(silence.StartTime)%60,
					int(silence.EndTime)/60, int(silence.EndTime)%60)
			}
			fmt.Printf("%02d:%02d-%02d:%02d %s: %s\n",
				int(sentence.StartTime)/60, int(sentence.StartTime)%60,
				int(sentence.EndTime)/60, int(sentence.EndTime)%60,
				sentence.Speaker, sentence.Content)
		}
	}
}
