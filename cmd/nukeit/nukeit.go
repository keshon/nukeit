package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

var guildID = "" // specify guild id for developing purpose (or command's names update will be fucked up)

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

	if err := b.openConnection(); err != nil {
		return fmt.Errorf("error opening connection: %w", err)
	}

	return nil
}

func (b *Bot) Shutdown() {
	if err := b.session.Close(); err != nil {
		fmt.Printf("Error closing Discord session: %v", err)
	}
}

func (b *Bot) openConnection() error {
	return b.session.Open()
}

func (b *Bot) configureIntents() {
	b.session.Identify.Intents = discordgo.IntentsAll
}

func (b *Bot) registerEventHandlers() {
	b.session.AddHandler(b.onReady)
	b.session.AddHandler(b.onMessageCreate)
	b.session.AddHandler(b.handleInteraction)
}

func (b *Bot) onReady(s *discordgo.Session, event *discordgo.Ready) {
	fmt.Printf("Logged in as %s", s.State.User.String())

	existingCommands, err := s.ApplicationCommands(s.State.User.ID, guildID)
	if err != nil {
		fmt.Printf("Error fetching commands: %v\n", err)
	} else {
		for _, cmd := range existingCommands {
			err := s.ApplicationCommandDelete(s.State.User.ID, "", cmd.ID)
			if err != nil {
				fmt.Printf("Error deleting command %s: %v\n", cmd.Name, err)
			} else {
				fmt.Printf("Deleted command: %s\n", cmd.Name)
			}
		}
	}

	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "nuke",
			Description: "Delete all messages in this channel",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "confirm",
					Description: "Type 'yes' to confirm the action",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    true,
				},
			},
		},
		{
			Name:        "nuke-to-date",
			Description: "Delete messages from now till a specific period",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "month",
					Description: "Select a month",
					Type:        discordgo.ApplicationCommandOptionInteger,
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "January", Value: 1},
						{Name: "February", Value: 2},
						{Name: "March", Value: 3},
						{Name: "April", Value: 4},
						{Name: "May", Value: 5},
						{Name: "June", Value: 6},
						{Name: "July", Value: 7},
						{Name: "August", Value: 8},
						{Name: "September", Value: 9},
						{Name: "October", Value: 10},
						{Name: "November", Value: 11},
						{Name: "December", Value: 12},
					},
				},
				{
					Name:        "year",
					Description: "Select a year",
					Type:        discordgo.ApplicationCommandOptionInteger,
					Required:    true,
					Choices:     generateYearChoices(),
				},
				{
					Name:        "confirm",
					Description: "Type 'yes' to confirm the action",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    true,
				},
			},
		},
	}

	for _, cmd := range commands {
		_, err := s.ApplicationCommandCreate(s.State.User.ID, guildID, cmd)
		if err != nil {
			fmt.Printf("Error creating command %s: %v\n", cmd.Name, err)
		} else {
			fmt.Printf("Registered command: %s\n", cmd.Name)
		}
	}
}

func generateYearChoices() []*discordgo.ApplicationCommandOptionChoice {
	currentYear := time.Now().Year()
	choices := []*discordgo.ApplicationCommandOptionChoice{}

	for year := currentYear - 5; year <= currentYear; year++ {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  fmt.Sprintf("%d", year),
			Value: year,
		})
	}
	return choices
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}
	// Print user guild id
	// fmt.Println(m.GuildID)
}

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.ApplicationCommandData().Name {
	case "nuke":
		b.handleDeleteAll(s, i)
	case "nuke-to-date":
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
			Content: "Deleting your messages in this channel...",
		},
	})
	b.deleteMessages(s, i.ChannelID, i.Member.User.ID, nil, nil)
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
	confirmation := options[2].Value.(string)

	if confirmation != "yes" {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You must type 'yes' to confirm the action.",
			},
		})
		return
	}

	startTime := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Now().UTC() //endTime := startTime.AddDate(0, 1, 0).Add(-time.Second)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("Deleting messages from %s to %s...", startTime.Format("January 2006"), endTime.Format("January 2006")),
		},
	})
	b.deleteMessages(s, i.ChannelID, i.Member.User.ID, &startTime, &endTime)
}

func (b *Bot) deleteMessages(s *discordgo.Session, channelID string, userID string, startTime, endTime *time.Time) {
	var lastMessageID string
	deletedCount := 0

	for {
		messages, err := s.ChannelMessages(channelID, 100, lastMessageID, "", "")
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
					fmt.Printf("Rate limit hit, sleeping for %.2f seconds\n", rateErr.RetryAfter.Seconds())
					time.Sleep(rateErr.RetryAfter)
					continue
				}
				fmt.Printf("Error deleting message: %v\n", err)
				continue
			}

			deletedCount++
			time.Sleep(300*time.Millisecond + time.Duration(rand.Intn(200))*time.Millisecond)
		}

		lastMessageID = messages[len(messages)-1].ID
		if len(messages) < 100 {
			break
		}
	}

	report := fmt.Sprintf("Deleted %d messages in channel <#%s>", deletedCount, channelID)
	if startTime != nil && endTime != nil {
		report += fmt.Sprintf(" from %s to %s.", startTime.Format(time.RFC822), endTime.Format(time.RFC822))
	} else {
		report += "."
	}

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

	if os.Getenv("TEST_GUILD_ID") == "" {
		fmt.Println("TEST_GUILD_ID is missing in environment variables. It is not mandatory and is used for development purpose")
	}

	guildID = os.Getenv("TEST_GUILD_ID")
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
