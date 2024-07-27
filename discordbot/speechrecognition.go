package discordbot

import (
	"context"
	"fmt"
	"jamie/db"
	"jamie/etc"
	"jamie/stt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func (bot *Bot) getRecognizersForStream(
	streamID string,
) ([]stt.SpeechRecognizer, error) {
	bot.mu.Lock()
	defer bot.mu.Unlock()

	recognizers, exists := bot.voiceCall.Recognizers[streamID]
	if !exists {
		recognizers = make([]stt.SpeechRecognizer, 0)
		
		// Start a default recognizer
		session, err := bot.speechRecognition.Start(context.Background())
		if err != nil {
			bot.log.Error(
				"Failed to start speech recognition session",
				"error",
				err,
				"streamID",
				streamID,
			)
			return nil, fmt.Errorf(
				"failed to start speech recognition session: %w",
				err,
			)
		}
		recognizers = append(recognizers, session)
		go bot.speechRecognitionLoop(streamID, session)

		// You can add more recognizers here for different languages or configurations
		// For example:
		// spanishSession, err := bot.speechRecognition.StartWithLanguage(context.Background(), "es")
		// if err == nil {
		//     recognizers = append(recognizers, spanishSession)
		//     go bot.speechRecognitionLoop(streamID, spanishSession)
		// }

		bot.voiceCall.Recognizers[streamID] = recognizers
	}
	return recognizers, nil
}

func (bot *Bot) speechRecognitionLoop(
	streamID string,
	session stt.SpeechRecognizer,
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
	drafts <-chan stt.Result,
) {
	var result stt.Result
	for draft := range drafts {
		result = draft
	}

	if result.Text != "" {
		row, err := bot.db.GetChannelAndUsernameForStream(
			context.Background(),
			streamID,
		)
		if err != nil {
			bot.log.Error(
				"Failed to get channel and username",
				"error", err.Error(),
				"streamID", streamID,
			)
			return
		}

		if bot.voiceCall != nil && bot.voiceCall.TalkMode {
			bot.speakingMu.Lock()
			isSpeaking := bot.isSpeaking
			bot.speakingMu.Unlock()

			if !isSpeaking {
				bot.speakingMu.Lock()
				bot.isSpeaking = true
				bot.speakingMu.Unlock()

				bot.handleTalkCommand(&discordgo.MessageCreate{
					Message: &discordgo.Message{
						ChannelID: row.DiscordChannel,
						Author: &discordgo.User{
							Username: row.Username,
						},
						Content: result.Text,
					},
				},
					strings.Fields(result.Text),
				)

				_, err = bot.discord.ChannelMessageSend(
					row.DiscordChannel,
					fmt.Sprintf("> %s: %s", row.Username, result.Text),
				)

				if err != nil {
					bot.log.Error(
						"Failed to send transcribed message in talk mode",
						"error", err.Error(),
						"channel", row.DiscordChannel,
					)
				}
			}
		} else {
			// Send the transcribed message as usual
			_, err = bot.discord.ChannelMessageSend(
				row.DiscordChannel,
				fmt.Sprintf("> %s: %s", row.Username, result.Text),
			)

			if err != nil {
				bot.log.Error(
					"Failed to send transcribed message",
					"error", err.Error(),
					"channel", row.DiscordChannel,
				)
			}
		}

		recognitionID := etc.Gensym()

		err = bot.db.SaveRecognition(
			context.Background(),
			db.SaveRecognitionParams{
				ID:         recognitionID,
				Stream:     streamID,
				SampleIdx:  int64(result.Start * 48000),
				SampleLen:  int64(result.Duration * 48000),
				Text:       result.Text,
				Confidence: result.Confidence,
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
