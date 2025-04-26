package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

type Bot struct {
	session *discordgo.Session
}

func NewBot(token string) (*Bot, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("error creating Discord session: %w", err)
	}
	return &Bot{session: s}, nil
}

func (b *Bot) Start() error {
	b.configureIntents()
	b.registerEventHandlers()

	return nil
}

func (b *Bot) Shutdown() {
	if err := b.session.Close(); err != nil {
		fmt.Printf("Error closing Discord session: %v", err)
	}
}

func (b *Bot) configureIntents() {
	b.session.Identify.Intents = discordgo.IntentsAll
}

func (b *Bot) registerEventHandlers() {
	b.session.AddHandler(b.onReady)
	b.session.AddHandler(b.handleInteraction)
}

func (b *Bot) onReady(s *discordgo.Session, event *discordgo.Ready) {
	fmt.Printf("Logged in as %s", s.State.User.String())

	// Register slash commands
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "deleteall",
			Description: "Delete all messages in this channel",
		},
		{
			Name:        "deleteperiod",
			Description: "Delete messages from a specific period",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "month",
					Description: "The month (1-12)",
					Type:        discordgo.ApplicationCommandOptionInteger,
					Required:    true,
				},
				{
					Name:        "year",
					Description: "The year (e.g., 2024)",
					Type:        discordgo.ApplicationCommandOptionInteger,
					Required:    true,
				},
			},
		},
	}

	for _, cmd := range commands {
		_, err := s.ApplicationCommandCreate(s.State.User.ID, "", cmd)
		if err != nil {
			fmt.Printf("Error creating command %s: %v", cmd.Name, err)
		}
	}
}

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.ApplicationCommandData().Name {
	case "deleteall":
		b.handleDeleteAll(s, i)
	case "deleteperiod":
		b.handleDeletePeriod(s, i)
	}
}

func (b *Bot) handleDeleteAll(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.isGuildOwner(i) {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You are not allowed to use this command.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Are you sure you want to delete all messages? Type 'yes' to confirm.",
		},
	})

	// Wait for confirmation
	b.awaitConfirmation(s, i, func() {
		b.deleteMessages(s, i.ChannelID, i.Member.User.ID, nil, nil)
	})
}

func (b *Bot) handleDeletePeriod(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.isGuildOwner(i) {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You are not allowed to use this command.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	options := i.ApplicationCommandData().Options
	month := int(options[0].Value.(float64))
	year := int(options[1].Value.(float64))

	startTime := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	endTime := startTime.AddDate(0, 1, 0).Add(-time.Second)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("Are you sure you want to delete messages from %s? Type 'yes' to confirm.", startTime.Format("January 2006")),
		},
	})

	// Wait for confirmation
	b.awaitConfirmation(s, i, func() {
		b.deleteMessages(s, i.ChannelID, i.Member.User.ID, &startTime, &endTime)
	})
}

func (b *Bot) awaitConfirmation(s *discordgo.Session, i *discordgo.InteractionCreate, onConfirm func()) {
	s.AddHandlerOnce(func(_ *discordgo.Session, confirmation *discordgo.MessageCreate) {
		if confirmation.Content == "yes" && confirmation.Author.ID == i.Member.User.ID {
			onConfirm()
		} else {
			s.ChannelMessageSend(i.ChannelID, "Confirmation canceled.")
		}
	})
}

func (b *Bot) deleteMessages(s *discordgo.Session, channelID string, userID string, startTime, endTime *time.Time) {
	var lastID string
	deletedCount := 0

	for {
		// Fetch messages in batches of 100
		messages, err := s.ChannelMessages(channelID, 100, lastID, "", "")
		if err != nil || len(messages) == 0 {
			break
		}

		for _, msg := range messages {
			if startTime != nil && msg.Timestamp.Before(*startTime) {
				continue
			}
			if endTime != nil && msg.Timestamp.After(*endTime) {
				continue
			}

			err := s.ChannelMessageDelete(channelID, msg.ID)
			if err != nil {
				if rateErr, ok := err.(*discordgo.RateLimitError); ok {
					// Ха! Поймали лимит! Уважаем, ждём
					fmt.Printf("Rate limit hit, sleeping for %.2f seconds\n", rateErr.RetryAfter.Seconds())
					time.Sleep(rateErr.RetryAfter)
					continue
				}
				fmt.Printf("Error deleting message: %v\n", err)
				continue
			}

			deletedCount++
			time.Sleep(200 * time.Millisecond) // Осторожничать всё ещё стоит
		}

		lastID = messages[len(messages)-1].ID
		if len(messages) < 100 {
			break
		}
	}

	// Send stats report to the user
	report := fmt.Sprintf("Deleted %d messages in channel <#%s>", deletedCount, channelID)
	if startTime != nil && endTime != nil {
		report += fmt.Sprintf(" from %s to %s.", startTime.Format(time.RFC822), endTime.Format(time.RFC822))
	} else {
		report += "."
	}

	// DM the user with the report
	dmChannel, err := s.UserChannelCreate(userID)
	if err == nil {
		s.ChannelMessageSend(dmChannel.ID, report)
	} else {
		fmt.Printf("Failed to DM user: %v", err)
	}
}

func (b *Bot) isGuildOwner(i *discordgo.InteractionCreate) bool {
	if i.GuildID == "" {
		return false
	}

	guild, err := b.session.Guild(i.GuildID)
	if err != nil {
		return false
	}

	return guild.OwnerID == i.Member.User.ID
}

func loadEnv(path string) {
	if err := godotenv.Load(path); err != nil {
		fmt.Println("Error loading .env file")
	}

	if os.Getenv("DISCORD_TOKEN") == "" {
		log.Fatal("DISCORD_TOKEN is missing in environment variables")
	}
}

func main() {
	loadEnv("./.env")

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("Discord token not found in environment variables")
	}

	bot, err := NewBot(token)
	if err != nil {
		log.Fatal("Failed to create bot:", err)
	}
	defer bot.Shutdown()

	if err := bot.Start(); err != nil {
		log.Fatal("Failed to start bot:", err)
	}

	fmt.Println("Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
	close(sc)
}
