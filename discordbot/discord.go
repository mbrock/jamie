package discordbot

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"jamie/db"
	"jamie/discordbot/tts"
	"jamie/etc"
	"jamie/llm"
	"jamie/ogg"
	"jamie/stt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	dis "github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
)

type CommandHandler func(*dis.Session, *dis.MessageCreate, []string) error

type Bot struct {
	mu   sync.Mutex
	db   *db.Queries
	log  *log.Logger
	conn *dis.Session

	languageModel llm.LanguageModel

	speechRecognition stt.SpeechRecognition
	speechRecognizers map[string]stt.SpeechRecognizer // streamID
	speechGenerator   tts.SpeechGenerator

	commands map[string]CommandHandler // command name

	voiceCall *VoiceCall

	isSpeaking bool
	speakingMu sync.Mutex

	guildID string
}

func NewBot(
	discordToken string,
	speechRecognitionService stt.SpeechRecognition,
	speechGenerationService tts.SpeechGenerator,
	languageModelService llm.LanguageModel,
	logger *log.Logger,
	db *db.Queries,
	guildID string,
) (*Bot, error) {
	bot := &Bot{
		db:                db,
		log:               logger,
		languageModel:     languageModelService,
		commands:          make(map[string]CommandHandler),
		speechRecognition: speechRecognitionService,
		speechRecognizers: make(
			map[string]stt.SpeechRecognizer,
		),
		speechGenerator: speechGenerationService,
		guildID:         guildID,
	}

	bot.registerCommands()

	dg, err := dis.New("Bot " + discordToken)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}

	dg.AddHandler(bot.handleGuildCreate)
	dg.AddHandler(bot.handleVoiceStateUpdate)
	dg.AddHandler(bot.handleMessageCreate)

	err = dg.Open()
	if err != nil {
		return nil, fmt.Errorf("error opening connection: %w", err)
	}

	bot.conn = dg
	bot.log.Info("bot", "username", bot.conn.State.User.Username)
	return bot, nil
}

func (bot *Bot) registerCommands() {
	bot.commands["talk"] = bot.handleTalkCommand
}

func (bot *Bot) Close() error {
	return bot.conn.Close()
}

func (bot *Bot) saveTextMessage(message *dis.Message) error {
	return bot.db.SaveTextMessage(
		context.Background(),
		db.SaveTextMessageParams{
			ID:               etc.Gensym(),
			DiscordChannel:   message.ChannelID,
			DiscordUser:      message.Author.ID,
			DiscordMessageID: message.ID,
			Content:          message.Content,
			IsBot:            message.Author.Bot,
		},
	)
}

func (bot *Bot) handleGuildCreate(_ *dis.Session, event *dis.GuildCreate) {
	bot.log.Info(
		"joined",
		"guild",
		event.Guild.Name,
		"id",
		event.Guild.ID,
	)
	if bot.guildID == "" || bot.guildID == event.Guild.ID {
		bot.joinAllVoiceChannels(event.Guild.ID)
	}
}

func (bot *Bot) handleMessageCreate(
	s *dis.Session,
	m *dis.MessageCreate,
) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	err := bot.saveTextMessage(m.Message)
	if err != nil {
		bot.log.Error("Failed to save received message", "error", err.Error())
	}

	bot.speakingMu.Lock()
	if bot.isSpeaking {
		bot.speakingMu.Unlock()
		bot.log.Debug("Ignoring command while speaking")
		return
	}
	bot.speakingMu.Unlock()

	if !strings.HasPrefix(m.Content, "!") {
		return
	}

	args := strings.Fields(m.Content[1:])
	if len(args) == 0 {
		return
	}

	commandName := args[0]
	if commandName == "talk" {
		if bot.voiceCall != nil {
			if bot.voiceCall.TalkMode {
				bot.voiceCall.TalkMode = false
				bot.sendAndSaveMessage(
					m.ChannelID,
					"Talk mode deactivated.",
				)
			} else {
				bot.voiceCall.TalkMode = true
				bot.sendAndSaveMessage(
					m.ChannelID,
					"Talk mode activated. Type !talk again to deactivate.",
				)
			}
		} else {
			bot.sendAndSaveMessage(
				m.ChannelID,
				"You must be in a voice channel to activate talk mode.",
			)
		}
		return
	}

	handler, exists := bot.commands[commandName]
	if !exists {
		bot.sendAndSaveMessage(
			m.ChannelID,
			fmt.Sprintf("Unknown command: %s", commandName),
		)
		return
	}

	err = handler(s, m, args[1:])
	if err != nil {
		bot.log.Error(
			"Command execution failed",
			"command",
			commandName,
			"error",
			err.Error(),
		)
		bot.sendAndSaveMessage(
			m.ChannelID,
			fmt.Sprintf("Error executing command: %s", err.Error()),
		)
	}
}

func (bot *Bot) sendAndSaveMessage(
	channelID, content string,
) {
	msg, err := bot.conn.ChannelMessageSend(channelID, content)
	if err != nil {
		bot.log.Error("Failed to send message", "error", err.Error())
		return
	}

	err = bot.saveTextMessage(msg)
	if err != nil {
		bot.log.Error("Failed to save sent message", "error", err.Error())
	}
}

func (bot *Bot) getUsernameFromID(userID string) string {
	user, err := bot.conn.User(userID)
	if err != nil {
		bot.log.Error(
			"Failed to get username",
			"userID",
			userID,
			"error",
			err,
		)
		return "Unknown User"
	}
	return user.Username
}

func (bot *Bot) UpdateMessageWithSummary(
	s *dis.Session,
	channelID string,
	messageID string,
	summaryChan <-chan string,
) string {
	var fullSummary strings.Builder
	updateTicker := time.NewTicker(2 * time.Second)
	defer updateTicker.Stop()

	for {
		select {
		case chunk, ok := <-summaryChan:
			if !ok {
				// Channel closed, summary generation complete
				return fullSummary.String()
			}
			fullSummary.WriteString(chunk)
		case <-updateTicker.C:
			if fullSummary.Len() > 0 {
				_, err := s.ChannelMessageEdit(
					channelID,
					messageID,
					fullSummary.String(),
				)
				if err != nil {
					bot.log.Error(
						"failed to update summary message",
						"error",
						err,
					)
				}
			}
		}
	}
}

func (bot *Bot) handleTalkCommand(
	_ *dis.Session,
	m *dis.MessageCreate,
	args []string,
) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: !talk <prompt>")
	}

	prompt := strings.Join(args, " ")

	// Start a goroutine to handle the command asynchronously
	go func() {
		response, err := bot.processTalkCommand(m, prompt)
		if err != nil {
			bot.log.Error("Failed to process talk command", "error", err)
			bot.sendAndSaveMessage(
				m.ChannelID,
				fmt.Sprintf("An error occurred: %v", err),
			)
			return
		}

		err = bot.speakInChannel(m.ChannelID, response)
		if err != nil {
			bot.log.Error("Failed to speak response", "error", err)
			bot.sendAndSaveMessage(
				m.ChannelID,
				fmt.Sprintf("Failed to speak the response: %v", err),
			)
		}

		bot.sendAndSaveMessage(m.ChannelID, response)
	}()

	return nil
}

func (bot *Bot) processTalkCommand(
	m *dis.MessageCreate,
	prompt string,
) (string, error) {
	messages, err := bot.db.GetRecentTextMessages(
		context.Background(),
		db.GetRecentTextMessagesParams{
			DiscordChannel: m.ChannelID,
			Limit:          100,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to fetch today's messages: %w", err)
	}

	recognitions, err := bot.db.GetRecentRecognitions(
		context.Background(),
		100,
	)
	if err != nil {
		return "", fmt.Errorf("failed to fetch recent recognitions: %w", err)
	}

	var contextBuilder strings.Builder
	contextBuilder.WriteString(
		"Context:\n\n",
	)

	type contextItem struct {
		content   string
		createdAt float64
	}
	var items []contextItem

	for _, msg := range messages {
		sender := "User"
		if msg.IsBot {
			sender = "Bot"
		}
		items = append(items, contextItem{
			content:   fmt.Sprintf("%s: %s", sender, msg.Content),
			createdAt: msg.CreatedAt,
		})
	}

	for _, rec := range recognitions {
		items = append(items, contextItem{
			content: fmt.Sprintf(
				"[%s UTC] %s: %s",
				etc.JulianDayToTime(rec.CreatedAt).UTC().
					Format("2006-01-02 15:04:05"),
				rec.DiscordUsername,
				rec.Text,
			),
			createdAt: rec.CreatedAt,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].createdAt < items[j].createdAt
	})

	for _, item := range items {
		contextBuilder.WriteString(item.content + "\n")
	}

	contextBuilder.WriteString("\n")
	contextBuilder.WriteString(
		fmt.Sprintf(
			"[%s UTC] %s: %s",
			m.Timestamp.Format("2006-01-02 15:04:05"),
			m.Author.Username,
			prompt,
		),
	)

	ctx := context.Background()

	response, err := bot.languageModel.ChatCompletion(
		ctx,
		(&llm.ChatCompletionRequest{
			SystemPrompt: "Briefly describe the latest conversation turn and its context, narratively, verbally, without any formatting.",
			MaxTokens:    300,
		}).WithUserMessage(contextBuilder.String()),
	)

	if err != nil {
		return "", fmt.Errorf(
			"failed to generate language model response: %w",
			err,
		)
	}

	var fullResponse strings.Builder
	for chunk := range response {
		if chunk.Err != nil {
			return "", fmt.Errorf(
				"error during response generation: %w",
				chunk.Err,
			)
		}
		fullResponse.WriteString(chunk.Content)
	}

	return fullResponse.String(), nil
}

func (bot *Bot) GenerateOggOpusBlob(
	streamID string,
	startSample, endSample int,
) ([]byte, error) {
	return ogg.GenerateOggOpusBlob(
		bot.db,
		streamID,
		int64(startSample),
		int64(endSample),
	)
}

func (bot *Bot) TextToSpeech(text string, writer io.Writer) error {
	bot.log.Info("speaking", "text", text)
	err := bot.speechGenerator.TextToSpeechStreaming(text, writer)
	if err != nil {
		bot.log.Error(
			"Failed to generate speech",
			"error",
			err,
		)
		return fmt.Errorf("failed to generate speech: %w", err)
	}

	return nil
}
