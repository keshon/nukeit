package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

var (
	guildID           string
	requireOwnerCheck bool
	activeDeletions   = make(map[string]chan struct{}) // channelID -> stop signal channel
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

	if err := b.session.Open(); err != nil {
		return fmt.Errorf("error opening connection: %w", err)
	}

	return nil
}

func (b *Bot) Shutdown() {
	if err := b.session.Close(); err != nil {
		fmt.Printf("Error closing Discord session: %v\n", err)
	}
}

func (b *Bot) configureIntents() {
	b.session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages
}

func (b *Bot) registerEventHandlers() {
	b.session.AddHandler(b.onReady)
	b.session.AddHandler(b.handleInteraction)
}

func (b *Bot) onReady(s *discordgo.Session, event *discordgo.Ready) {
	fmt.Printf("Logged in as %s\n", s.State.User.String())

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
			Description: "Delete messages from now until a specified date",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "month",
					Description: "Select a month",
					Type:        discordgo.ApplicationCommandOptionInteger,
					Required:    true,
					Choices:     generateMonthChoices(),
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
		{
			Name:        "stop",
			Description: "Stop an ongoing deletion process",
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

func generateMonthChoices() []*discordgo.ApplicationCommandOptionChoice {
	months := []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(months))
	for i, name := range months {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  name,
			Value: i + 1,
		})
	}
	return choices
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

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.ApplicationCommandData().Name {
	case "nuke":
		b.handleNuke(s, i)
	case "nuke-to-date":
		b.handleNukeToDate(s, i)
	case "stop":
		b.handleStop(s, i)
	}
}

func (b *Bot) hasPermission(s *discordgo.Session, guildID, channelID, userID string) (bool, string) {
	perms, err := s.UserChannelPermissions(userID, channelID)
	if err != nil {
		return false, "Failed to fetch permissions."
	}

	if requireOwnerCheck {
		guild, err := s.Guild(guildID)
		if err != nil {
			return false, "Failed to fetch guild info."
		}
		if guild.OwnerID == userID {
			return true, "User is the guild owner."
		}
		return false, "You must be the guild owner to execute this command."
	}

	if perms&discordgo.PermissionAdministrator != 0 {
		return true, "User has administrator permissions."
	}
	return false, "Administrator permission required."
}

func (b *Bot) handleNuke(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := i.ChannelID
	userID := i.Member.User.ID

	ok, reason := b.hasPermission(s, i.GuildID, channelID, userID)
	if !ok {
		b.respondEphemeral(s, i, reason)
		return
	}

	if !b.checkBotPermissions(s, channelID) {
		b.respondEphemeral(s, i, "I lack permissions to manage messages in this channel.")
		return
	}

	confirmation := i.ApplicationCommandData().Options[0].StringValue()
	if strings.ToLower(confirmation) != "yes" {
		b.respondEphemeral(s, i, "You must type 'yes' to confirm the action.")
		return
	}

	stopChan := make(chan struct{})
	activeDeletions[channelID] = stopChan

	b.respond(s, i, "Starting deletion...")

	go b.deleteMessages(s, channelID, nil, nil, stopChan)
}

func (b *Bot) handleNukeToDate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := i.ChannelID
	userID := i.Member.User.ID

	ok, reason := b.hasPermission(s, i.GuildID, channelID, userID)
	if !ok {
		b.respondEphemeral(s, i, reason)
		return
	}

	if !b.checkBotPermissions(s, channelID) {
		b.respondEphemeral(s, i, "I lack permissions to manage messages in this channel.")
		return
	}

	options := i.ApplicationCommandData().Options
	month := int(options[0].IntValue())
	year := int(options[1].IntValue())
	confirmation := options[2].StringValue()

	if strings.ToLower(confirmation) != "yes" {
		b.respondEphemeral(s, i, "You must type 'yes' to confirm the action.")
		return
	}

	startTime := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Now().UTC()

	stopChan := make(chan struct{})
	activeDeletions[channelID] = stopChan

	b.respond(s, i, fmt.Sprintf("Starting deletion from %s to %s...", startTime.Format(time.RFC822), endTime.Format(time.RFC822)))

	go b.deleteMessages(s, channelID, &startTime, &endTime, stopChan)
}

func (b *Bot) handleStop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := i.ChannelID

	if stopChan, ok := activeDeletions[channelID]; ok {
		close(stopChan)
		delete(activeDeletions, channelID)
		b.respond(s, i, "Deletion process stopped.")
	} else {
		b.respondEphemeral(s, i, "No active deletion process in this channel.")
	}
}

func (b *Bot) deleteMessages(s *discordgo.Session, channelID string, startTime, endTime *time.Time, stopChan <-chan struct{}) {
	var lastMessageID string
	deletedCount := 0

deletionLoop:
	for {
		select {
		case <-stopChan:
			fmt.Printf("Stopped deletion in channel %s\n", channelID)
			break deletionLoop
		default:
		}

		messages, err := s.ChannelMessages(channelID, 100, lastMessageID, "", "")
		if err != nil || len(messages) == 0 {
			break
		}

		for _, msg := range messages {
			select {
			case <-stopChan:
				fmt.Printf("Stopped deletion in channel %s\n", channelID)
				break deletionLoop
			default:
			}

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

	fmt.Printf("Deleted %d messages in channel %s\n", deletedCount, channelID)
}

func (b *Bot) checkBotPermissions(s *discordgo.Session, channelID string) bool {
	botID := s.State.User.ID
	perms, err := s.UserChannelPermissions(botID, channelID)
	if err != nil {
		return false
	}
	return perms&discordgo.PermissionManageMessages != 0
}

func (b *Bot) respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	})
}

func (b *Bot) respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func loadEnv(path string) {
	if err := godotenv.Load(path); err != nil {
		fmt.Println("Error loading .env file")
	}

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_TOKEN is missing in environment variables")
	}

	guildID = os.Getenv("TEST_GUILD_ID")

	checkMode := strings.ToLower(os.Getenv("CHECK_MODE"))
	requireOwnerCheck = (checkMode == "owner")
}

func main() {
	loadEnv("./.env")

	token := os.Getenv("DISCORD_TOKEN")

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

	fmt.Println("Shutting down...")
}
