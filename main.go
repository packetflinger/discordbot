package main

import (
	"flag"
	"fmt"
	"net/http"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

var (
	Token string
	Channels string
)

func init() {
	flag.StringVar(&Token, "t", "", "Bot Token")
	flag.StringVar(&Channels, "c", "", "The channels to listen in")
	flag.Parse()
}

func main() {

	bot, err := discordgo.New("Bot " + Token)
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return
	}

	bot.AddHandler(messageCreate)

	// we only care about receiving message events.
	bot.Identify.Intents = discordgo.IntentsGuildMessages

	// Open a websocket connection to Discord and begin listening.
	err = bot.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	bot.Close()
}

//
// Called for every message seen
//
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {

	// Ignore our outgoing messages
	if m.Author.ID == s.State.User.ID {
		return
	}

	// testing
	if m.Content == "ping" {
		s.ChannelMessageSend(m.ChannelID, "Pong!")
	}

	// only process attachments in designated channels
	if m.ChannelID == Channels {
		if len(m.Attachments) > 0 {
			for _, v := range m.Attachments {
				filename, valid := ValidateFile(strings.ToLower(v.URL))
				if !valid {
					s.ChannelMessageSend(m.ChannelID, "Invalid file, ignoring.")
					return
				}

				DownloadFile("./"+filename, v.URL)
			}
		}
	}
}

//
// Make sure the attached file is valid for processing:
// - .bsp, .zip, .pak, .pkz only
//
// returns: filename, boolean for acceptable or not
//
func ValidateFile(url string) (string, bool) {
	validexts := []string{"bsp","zip","pak","pkz"}

	parts := strings.Split(url,  "/")
	if len(parts) < 1 {
		return "", false
	}

	filename := parts[len(parts)-1]

	parts2 := strings.Split(filename, ".")
	if len(parts2) < 2 {
		return "", false
	}

	extension := parts2[1]

	return filename, Contains(extension, validexts)
}

//
// Actually download the file from discord's CDN
//
func DownloadFile(filepath string, url string) error {

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the data to file
	_, err = io.Copy(out, resp.Body)
	return err
}

//
// helper function, if string is in slice
//
func Contains(needle string, haystack []string) bool {
	for i := range haystack {
		if needle == haystack[i] {
			return true
		}
	}

	return false
}
