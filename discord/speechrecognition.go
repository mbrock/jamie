package discord

import (
	"context"
	"fmt"
	"jamie/ai"
	"jamie/db"
	"jamie/etc"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (bot *Bot) getRecognizersForStream(streamID string) ([]ai.SpeechRecognitionSession, error) {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	recognizers, exists := bot.voiceChat.Transcribers[streamID]
	if exists {
		return recognizers, nil
	}

	recognizers = make([]ai.SpeechRecognitionSession, 0)

	if err := bot.addRecognizer(streamID, &recognizers, "en-US"); err != nil {
		return nil, err
	}

	_ = bot.addRecognizer(streamID, &recognizers, "sv-SE")

	bot.voiceChat.Transcribers[streamID] = recognizers
	return recognizers, nil
}

func (bot *Bot) addRecognizer(streamID string, recognizers *[]ai.SpeechRecognitionSession, language string) error {
	firstSampleIdx, err := bot.db.GetVoiceStreamEarliestSampleIdx(context.Background(), streamID)
	if err != nil {
		return fmt.Errorf("failed to get earliest sample index: %w", err)
	}

	session, err := bot.speechRecognitionService.Start(context.Background(), language)
	if err != nil {
		return fmt.Errorf("failed to start %s speech recognition session: %w", language, err)
	}

	deepgramSession, ok := session.(*ai.DeepgramSession)
	if !ok {
		return fmt.Errorf("unexpected session type")
	}

	deepgramSession.InitialSampleIndex = int(firstSampleIdx)

	*recognizers = append(*recognizers, deepgramSession)
	go bot.speechRecognitionLoop(streamID, deepgramSession)
	return nil
}

func (bot *Bot) speechRecognitionLoop(
	streamID string,
	session *ai.DeepgramSession,
) {
	for segmentDrafts := range session.Receive() {
		bot.processPendingRecognitionResult(streamID, segmentDrafts)
	}

	bot.log.Info(
		"Speech recognition session closed",
		"streamID",
		streamID,
	)
}

func (bot *Bot) processPendingRecognitionResult(
	streamID string,
	drafts <-chan ai.Result,
) {
	var result ai.Result
	for draft := range drafts {
		result = draft
	}

	// Define a confidence threshold (e.g., 0.7)
	const confidenceThreshold = 0.7

	if result.Text != "" && result.Confidence >= confidenceThreshold {
		row, err := bot.db.GetVoiceStreamDiscordInfo(context.Background(), streamID)
		if err != nil {
			bot.log.Error(
				"Failed to get voice stream discord info",
				"error", err.Error(),
				"streamID", streamID,
			)
			return
		}
		bot.lastValidTranscription = time.Now()

		if bot.voiceChat != nil && bot.voiceChat.TalkMode {
			bot.speakingMu.Lock()
			isSpeaking := bot.isSpeaking
			bot.speakingMu.Unlock()

			if !isSpeaking {
				bot.speakingMu.Lock()
				bot.isSpeaking = true
				bot.speakingMu.Unlock()

				bot.handleTalkCommand(&discordgo.MessageCreate{
					Message: &discordgo.Message{
						ChannelID: row.DiscordChannelID,
						Author: &discordgo.User{
							ID:       row.DiscordUserID,
							Username: bot.getUsernameFromID(row.DiscordUserID),
						},
						Content: result.Text,
					},
				},
					strings.Fields(result.Text),
				)

				_, err = bot.discord.ChannelMessageSend(
					row.DiscordChannelID,
					fmt.Sprintf("> %s: %s", bot.getUsernameFromID(row.DiscordUserID), result.Text),
				)

				if err != nil {
					bot.log.Error(
						"Failed to send transcribed message in talk mode",
						"error", err.Error(),
						"channel", row.DiscordChannelID,
					)
				}
			}
		} else {
			// Send the transcribed message as usual
			_, err = bot.discord.ChannelMessageSend(
				row.DiscordChannelID,
				fmt.Sprintf("> %s: %s (Confidence: %.2f)", bot.getUsernameFromID(row.DiscordUserID), result.Text, result.Confidence),
			)

			if err != nil {
				bot.log.Error(
					"Failed to send transcribed message",
					"error", err.Error(),
					"channel", row.DiscordChannelID,
				)
			}
		}
	}

	if result.Confidence < confidenceThreshold {
		bot.log.Info(
			"Rejected transcription due to low confidence",
			"text", result.Text,
			"confidence", result.Confidence,
		)
	} else {
		recognitionID := etc.NewFreshID()

		err := bot.db.InsertRecognitionResult(
			context.Background(),
			db.InsertRecognitionResultParams{
				ID:          recognitionID,
				StartSecond: result.Start,
				EndSecond:   result.Start + result.Duration,
				Text:        result.Text,
				Confidence:  result.Confidence,
			},
		)
		if err != nil {
			bot.log.Error(
				"Failed to save recognition to database",
				"error", err.Error(),
				"recognitionID", recognitionID,
			)
		}
	}
}
