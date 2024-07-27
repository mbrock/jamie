package discordbot

import (
	"context"
	"fmt"
	"jamie/db"
	"jamie/discordbot/tts"
	"jamie/etc"
	"jamie/llm"
	"jamie/ogg"
	"jamie/stt"
	"sort"
	"strings"
	"sync"
	"time"

	dis "github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
	"github.com/sashabaranov/go-openai"
)

type CommandHandler func(*dis.Session, *dis.MessageCreate, []string) error

type Bot struct {
	mu   sync.Mutex
	db   *db.Queries
	log  *log.Logger
	conn *dis.Session

	openaiAPIKey string

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
	logger *log.Logger,
	openaiAPIKey string,
	db *db.Queries,
	guildID string,
) (*Bot, error) {
	bot := &Bot{
		db:                db,
		log:               logger,
		openaiAPIKey:      openaiAPIKey,
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
	bot.commands["summary"] = bot.handleSummaryCommand
	bot.commands["prompt"] = bot.handlePromptCommand
	bot.commands["listprompts"] = bot.handleListPromptsCommand
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
		"joined guild",
		"name",
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

func (bot *Bot) handleSummaryCommand(
	s *dis.Session,
	m *dis.MessageCreate,
	args []string,
) error {
	bot.log.Info(
		"Summary command received",
		"channel",
		m.ChannelID,
		"args",
		args,
	)

	if len(args) < 1 {
		return fmt.Errorf("usage: !summary [prompt_name] [speak]")
	}

	var promptName string
	var speak bool
	if len(args) > 0 {
		promptName = args[0]
	}
	if len(args) > 1 && args[1] == "speak" {
		speak = true
	}

	// Generate summary
	summaryChan, err := llm.SummarizeTranscript(
		bot.db,
		bot.openaiAPIKey,
		promptName,
	)
	if err != nil {
		return fmt.Errorf("failed to start summary generation: %w", err)
	}

	// Send initial message
	message, err := s.ChannelMessageSend(m.ChannelID, "Generating summary...")
	if err != nil {
		return fmt.Errorf("failed to send initial message: %w", err)
	}

	// Update the message with the summary
	fullSummary := bot.updateMessageWithSummary(
		s,
		m.ChannelID,
		message.ID,
		summaryChan,
	)

	// Save the final summary message
	err = bot.saveTextMessage(message)
	if err != nil {
		bot.log.Error(
			"Failed to save final summary message",
			"error",
			err.Error(),
		)
	}

	if speak {
		err = bot.speakSummary(s, m, fullSummary)
		if err != nil {
			return fmt.Errorf("failed to speak summary: %w", err)
		}
	}

	return nil
}

func (bot *Bot) updateMessageWithSummary(
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

func (bot *Bot) handlePromptCommand(
	s *dis.Session,
	m *dis.MessageCreate,
	args []string,
) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: !prompt <name> <prompt text>")
	}

	name := args[0]
	prompt := strings.Join(args[1:], " ")

	err := bot.db.SetSystemPrompt(
		context.Background(),
		db.SetSystemPromptParams{
			Name:   name,
			Prompt: prompt,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to set system prompt: %w", err)
	}

	bot.sendAndSaveMessage(
		m.ChannelID,
		fmt.Sprintf("System prompt '%s' has been set.", name),
	)

	return nil
}

func (bot *Bot) handleListPromptsCommand(
	s *dis.Session,
	m *dis.MessageCreate,
	args []string,
) error {
	prompts, err := bot.db.ListSystemPrompts(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list system prompts: %w", err)
	}

	if len(prompts) == 0 {
		bot.sendAndSaveMessage(
			m.ChannelID,
			"No system prompts have been set.",
		)
		return nil
	}

	var message strings.Builder
	message.WriteString("Available system prompts:\n")
	for _, prompt := range prompts {
		message.WriteString(
			fmt.Sprintf("- %s: %s\n", prompt.Name, prompt.Prompt),
		)
	}

	bot.sendAndSaveMessage(m.ChannelID, message.String())

	return nil
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
	// Fetch today's text messages and recognitions
	messages, err := bot.db.GetRecentTextMessages(
		context.Background(),
		db.GetRecentTextMessagesParams{
			DiscordChannel: m.ChannelID,
			Limit:          50,
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to fetch today's messages: %w", err)
	}

	recognitions, err := bot.db.GetRecentRecognitions(
		context.Background(),
		50,
	)
	if err != nil {
		return "", fmt.Errorf("failed to fetch recent recognitions: %w", err)
	}

	// Create context from today's messages and recognitions
	var contextBuilder strings.Builder
	contextBuilder.WriteString(
		"Today's conversation and voice transcriptions:\n",
	)

	// Combine and sort messages and recognitions
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
			content:   fmt.Sprintf("%s: %s", rec.Emoji, rec.Text),
			createdAt: rec.CreatedAt,
		})
	}

	// Sort items by createdAt
	sort.Slice(items, func(i, j int) bool {
		return items[i].createdAt < items[j].createdAt
	})

	// Add sorted items to context
	for _, item := range items {
		contextBuilder.WriteString(item.content + "\n")
	}

	contextBuilder.WriteString(
		"\nBased on the conversation and voice transcriptions above, react to the following prompt. Remember, you are a brief, terse, stoner noir, weird interlocutor named Jamie. You never offer to help. You improvise together. Respond without using any markup or formatting, as your response will be sent to a text-to-speech service.\n",
	)
	contextBuilder.WriteString(prompt)

	// Create OpenAI client
	client := openai.NewClient(bot.openaiAPIKey)
	ctx := context.Background()

	// Generate response using GPT-4
	resp, err := client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model:     openai.GPT4o,
			MaxTokens: 300,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You never offer to help. You hang back. Write terse, weird analyses without formatting. Do not react, just write.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: contextBuilder.String(),
				},
			},
		},
	)

	if err != nil {
		return "", fmt.Errorf("failed to generate GPT-4 response: %w", err)
	}

	return resp.Choices[0].Message.Content, nil
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

func (bot *Bot) TextToSpeech(text string) ([]byte, error) {
	bot.log.Info("speaking", "text", text)
	audio, err := bot.speechGenerator.TextToSpeech(text)
	if err != nil {
		bot.log.Error(
			"Failed to generate speech",
			"error",
			err,
		)
		return nil, fmt.Errorf("failed to generate speech: %w", err)
	}

	return audio, nil
}

func (bot *Bot) speakSummary(
	s *dis.Session,
	m *dis.MessageCreate,
	summary string,
) error {
	// Find the voice channel the user is in
	guild, err := s.State.Guild(m.GuildID)
	if err != nil {
		return fmt.Errorf("failed to get guild: %w", err)
	}

	var voiceChannelID string
	for _, vs := range guild.VoiceStates {
		if vs.UserID == m.Author.ID {
			voiceChannelID = vs.ChannelID
			break
		}
	}

	if voiceChannelID == "" {
		return fmt.Errorf("user is not in a voice channel")
	}

	// Join the voice channel if not already connected
	bot.mu.Lock()
	if bot.voiceCall == nil ||
		bot.voiceCall.Conn.ChannelID != voiceChannelID {
		err := bot.joinVoiceCall(m.GuildID, voiceChannelID)
		if err != nil {
			bot.mu.Unlock()
			return fmt.Errorf("failed to join voice channel: %w", err)
		}
	}
	voiceChannel := bot.voiceCall
	bot.mu.Unlock()

	// Generate speech
	speechData, err := bot.TextToSpeech(summary)
	if err != nil {
		return fmt.Errorf("failed to generate speech: %w", err)
	}

	// Convert to Opus packets
	opusPackets, err := ogg.ConvertToOpus(speechData)
	if err != nil {
		return fmt.Errorf("failed to convert to Opus: %w", err)
	}

	// Send Opus packets
	voiceChannel.Conn.Speaking(true)
	defer voiceChannel.Conn.Speaking(false)

	for _, packet := range opusPackets {
		voiceChannel.Conn.OpusSend <- packet
	}

	return nil
}
