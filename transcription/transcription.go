package transcription

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/iterator"
)

type TranscriptionManager struct {
	model            *genai.GenerativeModel
	output           io.Writer
	history          []string
	previousAudioURI string
}

func (tm *TranscriptionManager) SetPreviousAudioURI(uri string) {
	tm.previousAudioURI = uri
}

func NewTranscriptionManager(
	client *genai.Client,
	output io.Writer,
	existingTranscripts []string,
) *TranscriptionManager {
	model := setupGenerativeModel(client)
	return &TranscriptionManager{
		model:   model,
		output:  output,
		history: existingTranscripts,
	}
}

func setupGenerativeModel(client *genai.Client) *genai.GenerativeModel {
	model := client.GenerativeModel("gemini-1.5-pro")
	model.GenerationConfig.SetMaxOutputTokens(8192)
	model.GenerationConfig.SetTemperature(0.1)
	model.GenerationConfig.SetTopP(1.0)
	systemPrompt := `Transcribe this voice chat segment as accurately as possible, with good grammar and punctuation.

		Use double newlines to separate sentences.`
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{
			genai.Text(systemPrompt),
		},
	}
	log.Println("System Prompt:")
	log.Println(systemPrompt)
	model.SafetySettings = []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: genai.HarmBlockOnlyHigh,
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: genai.HarmBlockOnlyHigh,
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: genai.HarmBlockNone,
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: genai.HarmBlockOnlyHigh,
		},
	}
	return model
}

func (tm *TranscriptionManager) TranscribeSegment(
	ctx context.Context,
	audioURI string,
	isResuming bool,
) error {
	var prompt []genai.Part
	if len(tm.history) == 0 && !isResuming {
		prompt = buildPrompt(
			[]genai.Part{tm.model.SystemInstruction.Parts[0]},
			audioSegment(audioURI),
		)
	} else {
		prompt = buildPrompt(
			[]genai.Part{tm.model.SystemInstruction.Parts[0]},
			previousAudioSegment(tm.previousAudioURI),
			[]genai.Part{previousSegments(tm.history, 1)},
			audioSegment(audioURI),
			[]genai.Part{continuationInstruction()},
		)
	}

	// Log the prompt being sent
	log.Println("Sending prompt:")
	for _, part := range prompt {
		switch v := part.(type) {
		case genai.Text:
			log.Printf("Text: %s\n", string(v))
		case genai.FileData:
			log.Printf("FileData: URI=%s, MIMEType=%s\n", v.URI, v.MIMEType)
		default:
			log.Printf("Unknown part type: %T\n", v)
		}
	}

	stream := tm.model.GenerateContentStream(ctx, prompt...)

	var currentSegment strings.Builder
	for {
		resp, err := stream.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("error streaming: %w", err)
		}

		chunk := getResponseText(resp)

		if _, err := io.WriteString(tm.output, chunk); err != nil {
			return fmt.Errorf("error writing to output: %w", err)
		}
		currentSegment.WriteString(chunk)
	}

	tm.history = append(tm.history, currentSegment.String())
	tm.previousAudioURI = audioURI
	return nil
}

// Prompt building functions

func buildPrompt(partGroups ...[]genai.Part) []genai.Part {
	var allParts []genai.Part
	for _, group := range partGroups {
		allParts = append(allParts, group...)
	}
	return allParts
}

func previousSegments(history []string, count int) genai.Part {
	if count > len(history) {
		count = len(history)
	}
	segments := history[len(history)-count:]
	var sb strings.Builder
	for _, segment := range segments {
		sb.WriteString("Previous transcript:\n\n")
		sb.WriteString(segment)
		sb.WriteString("\n\n")
	}
	return genai.Text(sb.String())
}

func audioSegment(uri string) []genai.Part {
	return []genai.Part{
		genai.Text("<audio>\n"),
		genai.FileData{URI: uri, MIMEType: "audio/mp3"},
		genai.Text("</audio>\n"),
	}
}

func previousAudioSegment(uri string) []genai.Part {
	if uri == "" {
		return nil
	}
	return []genai.Part{
		genai.Text("Previous audio:\n"),
		genai.FileData{URI: uri, MIMEType: "audio/opus"},
	}
}

func continuationInstruction() genai.Part {
	return genai.Text("\n\n---\nContinue transcribing.")
}

func getResponseText(resp *genai.GenerateContentResponse) string {
	var text strings.Builder
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				if t, ok := part.(genai.Text); ok {
					text.WriteString(string(t))
				}
			}
		}
	}
	return text.String()
}
