package main

import (
	"fmt"
	"os"
	"os/signal"
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
