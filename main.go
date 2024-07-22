package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/charmbracelet/log"
)

var (
	Token string
	logger *log.Logger
)

func init() {
	Token = os.Getenv("DISCORD_TOKEN")
	if Token == "" {
		fmt.Println("No token provided. Please set the DISCORD_TOKEN environment variable.")
		os.Exit(1)
	}

	logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportCaller: true,
		ReportTimestamp: true,
	})
}

func main() {
	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		logger.Fatal("Error creating Discord session", "error", err)
	}

	dg.AddHandler(messageCreate)
	dg.AddHandler(commandHandler)

	err = dg.Open()
	if err != nil {
		logger.Fatal("Error opening connection", "error", err)
	}

	logger.Info("Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	dg.Close()
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	logger.Info("Received message", 
		"content", m.Content,
		"author", m.Author.Username,
		"channel", m.ChannelID,
	)
}

func commandHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if strings.HasPrefix(m.Content, "!join") {
		channelName := strings.TrimSpace(strings.TrimPrefix(m.Content, "!join"))
		voiceChannel, err := findVoiceChannel(s, m.GuildID, channelName)
		if err != nil {
			logger.Error("Error finding voice channel", "error", err)
			s.ChannelMessageSend(m.ChannelID, "Error: "+err.Error())
			return
		}

		_, err = s.ChannelVoiceJoin(m.GuildID, voiceChannel.ID, false, false)
		if err != nil {
			logger.Error("Error joining voice channel", "error", err)
			s.ChannelMessageSend(m.ChannelID, "Failed to join voice channel.")
			return
		}

		logger.Info("Joined voice channel", "channel", voiceChannel.Name)
		s.ChannelMessageSend(m.ChannelID, "Joined voice channel: "+voiceChannel.Name)
	}
}

func findVoiceChannel(s *discordgo.Session, guildID, channelName string) (*discordgo.Channel, error) {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return nil, fmt.Errorf("error getting guild channels: %w", err)
	}

	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildVoice && strings.EqualFold(channel.Name, channelName) {
			return channel, nil
		}
	}

	return nil, fmt.Errorf("voice channel '%s' not found", channelName)
}
