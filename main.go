package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"

	api "github.com/deepgram/deepgram-go-sdk/pkg/api/listen/v1/websocket/interfaces"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/pkg/client/listen"
)

type channelInfo struct {
	ID   string
	Name string
}

type model struct {
	channels     []channelInfo
	activeTab    int
	transcripts  map[string]*list.Model
	quitting     bool
	channelMutex sync.Mutex
}

var (
	Token         string
	logger        *log.Logger
	DeepgramToken string
)

func init() {
	Token = os.Getenv("DISCORD_TOKEN")
	if Token == "" {
		fmt.Println("No Discord token provided. Please set the DISCORD_TOKEN environment variable.")
		os.Exit(1)
	}

	DeepgramToken = os.Getenv("DEEPGRAM_API_KEY")
	if DeepgramToken == "" {
		fmt.Println("No Deepgram token provided. Please set the DEEPGRAM_API_KEY environment variable.")
		os.Exit(1)
	}

	logger = log.NewWithOptions(io.Discard, log.Options{
		ReportCaller:    true,
		ReportTimestamp: true,
	})
}

func main() {
	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		logger.Fatal("Error creating Discord session", "error", err.Error())
	}

	m := initialModel()
	dg.AddHandler(func(s *discordgo.Session, event *discordgo.GuildCreate) {
		guildCreate(s, event, &m)
	})

	err = dg.Open()
	if err != nil {
		logger.Fatal("Error opening connection", "error", err.Error())
	}

	p := tea.NewProgram(&m, tea.WithAltScreen())

	go func() {
		if _, err := p.Run(); err != nil {
			logger.Fatal("Error running program", "error", err.Error())
		}
	}()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	dg.Close()
}

func initialModel() model {
	return model{
		channels:    []channelInfo{},
		activeTab:   0,
		transcripts: make(map[string]*list.Model),
	}
}

func startDeepgramStream(v *discordgo.VoiceConnection, guildID, channelID string, m *model) {
	logger.Info("Starting Deepgram stream", "guild", guildID, "channel", channelID)

	// Initialize Deepgram client
	ctx := context.Background()
	cOptions := &interfaces.ClientOptions{
		EnableKeepAlive: true,
	}
	tOptions := &interfaces.LiveTranscriptionOptions{
		Model:          "nova-2",
		Language:       "en-US",
		Punctuate:      true,
		Encoding:       "opus",
		Channels:       2,
		SampleRate:     48000,
		SmartFormat:    true,
		InterimResults: true,
		UtteranceEndMs: "1000",
	}

	callback := MyCallback{
		sb:        &strings.Builder{},
		model:     m,
		channelID: channelID,
	}

	dgClient, err := client.NewWebSocket(ctx, DeepgramToken, cOptions, tOptions, callback)
	if err != nil {
		logger.Error("Error creating LiveTranscription connection", "error", err.Error())
		return
	}

	bConnected := dgClient.Connect()
	if !bConnected {
		logger.Error("Failed to connect to Deepgram")
		return
	}

	// Start receiving audio
	v.Speaking(true)
	defer v.Speaking(false)

	for {
		opus, ok := <-v.OpusRecv
		if !ok {
			logger.Info("Voice channel closed")
			break
		}
		err := dgClient.WriteBinary(opus.Opus)
		if err != nil {
			logger.Error("Failed to send audio to Deepgram", "error", err.Error())
		}
	}

	dgClient.Stop()
}

func voiceStateUpdate(s *discordgo.VoiceConnection, v *discordgo.VoiceSpeakingUpdate) {
	logger.Info("Voice state update", "userID", v.UserID, "speaking", v.Speaking)
}

func guildCreate(s *discordgo.Session, event *discordgo.GuildCreate, m *model) {
	logger.Info("Joined new guild", "guild", event.Guild.Name)
	err := joinAllVoiceChannels(s, event.Guild.ID, m)
	if err != nil {
		logger.Error("Error joining voice channels", "error", err.Error())
	}
}

func joinAllVoiceChannels(s *discordgo.Session, guildID string, m *model) error {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}

	m.channelMutex.Lock()
	defer m.channelMutex.Unlock()

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice {
			vc, err := s.ChannelVoiceJoin(guildID, channel.ID, false, false)
			if err != nil {
				logger.Error("Failed to join voice channel", "channel", channel.Name, "error", err.Error())
			} else {
				logger.Info("Joined voice channel", "channel", channel.Name)
				m.channels = append(m.channels, channelInfo{ID: channel.ID, Name: channel.Name})
				newList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
				m.transcripts[channel.ID] = &newList
				go startDeepgramStream(vc, guildID, channel.ID, m)
			}

			vc.AddHandler(voiceStateUpdate)
		}
	}

	return nil
}

type MyCallback struct {
	sb        *strings.Builder
	model     *model
	channelID string
}

func (c MyCallback) Message(mr *api.MessageResponse) error {
	sentence := strings.TrimSpace(mr.Channel.Alternatives[0].Transcript)

	if len(mr.Channel.Alternatives) == 0 || len(sentence) == 0 {
		return nil
	}

	if mr.IsFinal {
		c.sb.WriteString(sentence)
		c.sb.WriteString(" ")

		if mr.SpeechFinal {
			c.model.channelMutex.Lock()
			transcript := c.sb.String()
			c.model.transcripts[c.channelID].InsertItem(0, item{title: transcript, description: ""})
			c.model.channelMutex.Unlock()
			c.sb.Reset()
		}
	}

	return nil
}

func (c MyCallback) Open(ocr *api.OpenResponse) error {
	return nil
}

func (c MyCallback) Metadata(md *api.MetadataResponse) error {
	return nil
}

func (c MyCallback) SpeechStarted(ssr *api.SpeechStartedResponse) error {
	return nil
}

func (c MyCallback) UtteranceEnd(ur *api.UtteranceEndResponse) error {
	utterance := strings.TrimSpace(c.sb.String())
	if len(utterance) > 0 {
		c.model.channelMutex.Lock()
		c.model.transcripts[c.channelID].InsertItem(0, item{title: utterance, description: "[Utterance End]"})
		c.model.channelMutex.Unlock()
		c.sb.Reset()
	}
	return nil
}

func (c MyCallback) Close(ocr *api.CloseResponse) error {
	return nil
}

func (c MyCallback) Error(er *api.ErrorResponse) error {
	c.model.channelMutex.Lock()
	c.model.transcripts[c.channelID].InsertItem(0, item{title: "Error", description: er.Description})
	c.model.channelMutex.Unlock()
	return nil
}

func (c MyCallback) UnhandledEvent(byData []byte) error {
	return nil
}

type item struct {
	title, description string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.description }
func (i item) FilterValue() string { return i.title }

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			m.activeTab = (m.activeTab + 1) % len(m.channels)
		case "shift+tab":
			m.activeTab = (m.activeTab - 1 + len(m.channels)) % len(m.channels)
		}

	case tea.WindowSizeMsg:
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		for _, list := range m.transcripts {
			list.SetSize(msg.Width-h, msg.Height-v-4)
		}
	}

	if len(m.channels) > 0 {
		m.channelMutex.Lock()
		activeList := m.transcripts[m.channels[m.activeTab].ID]
		updatedList, cmd := activeList.Update(msg)
		*m.transcripts[m.channels[m.activeTab].ID] = updatedList
		m.channelMutex.Unlock()
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	var content string
	if len(m.channels) > 0 {
		m.channelMutex.Lock()
		activeList := m.transcripts[m.channels[m.activeTab].ID]
		content = activeList.View()
		m.channelMutex.Unlock()
	} else {
		content = "No channels available"
	}

	tabs := ""
	for i, channel := range m.channels {
		style := lipgloss.NewStyle().Padding(0, 1)
		if i == m.activeTab {
			style = style.Background(lipgloss.Color("205")).Foreground(lipgloss.Color("0"))
		}
		tabs += style.Render(channel.Name) + " "
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		tabs,
		content,
	)
}
