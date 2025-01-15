package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/packetflinger/libq2/bsp"
	"github.com/packetflinger/libq2/pak"
	"github.com/packetflinger/libq2/proto"
	"github.com/packetflinger/libq2/state"
	"google.golang.org/protobuf/encoding/prototext"

	pb "github.com/packetflinger/discordbot/proto"
)

var (
	configFile = flag.String("config", "", "Protobuf config file")
	config     *pb.BotConfig
	err        error
	fileTypes  = []string{".bsp", ".pak", ".pkz", ".zip"}
)

func main() {
	flag.Parse()
	config, err = loadConfig(*configFile)
	if err != nil {
		log.Fatalln("error loading config:", err)
	}
	if !config.GetForeground() {
		f, err := os.OpenFile(config.GetLogFile(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			log.Fatalf("error opening log file: %v", err)
		}
		log.SetOutput(f)
		defer f.Close()
	}
	if config.TempPath == "" {
		config.TempPath = path.Join(os.TempDir(), "discordbot")
	}
	if _, err := os.Stat(config.TempPath); os.IsNotExist(err) {
		err := os.Mkdir(config.TempPath, 0700)
		if err != nil {
			log.Fatalf("unable to create temp directory: %v\n", err)
		}
	}
	log.Printf("using %q for temp space", config.TempPath)

	if _, err := os.Stat(config.GetRepoPath()); os.IsNotExist(err) {
		if err != nil {
			log.Fatalf("repo path error: %v\n", err)
		}
	}

	bot, err := discordgo.New("Bot " + config.GetAuthToken())
	if err != nil {
		log.Fatalln("error creating Discord session:", err)
	}
	bot.AddHandler(handleMessage)

	// we only care about receiving message events.
	bot.Identify.Intents = discordgo.IntentsGuildMessages

	err = bot.Open()
	if err != nil {
		log.Fatalln("error opening connection,", err)
	}
	log.Printf("Discord bot running...\n")

	// Wait here until CTRL-C or other term signal is received.
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	bot.Close()
}

// loadConfig will read the textproto config file
//
// The default config file is $HOME/.config/discordbot/config.pb
func loadConfig(cf string) (*pb.BotConfig, error) {
	if cf == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		cf = path.Join(home, ".config", "discordbot", "config.pb")
	}
	configData, err := os.ReadFile(cf)
	if err != nil {
		return nil, err
	}
	var config pb.BotConfig
	err = prototext.Unmarshal(configData, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// Called for every message seen
func handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore our outgoing messages
	if m.Author.ID == s.State.User.ID {
		return
	}
	if len(m.Content) > 0 {
		handleMessageText(s, m)
	}
	if len(m.Attachments) > 0 {
		handleMessageAttachments(s, m)
	}
}

// handleMessageText will process and respond to channel posts that have a
// message containing text. Our own replies are filtered out before this is
// called.
func handleMessageText(s *discordgo.Session, m *discordgo.MessageCreate) {
	tokens := strings.Fields(m.Content)
	if len(tokens) < 2 {
		return
	}
	if tokens[0] == "!q2" && Contains(m.ChannelID, config.GetStatusChannels()) {
		go func() {
			log.Printf("%s[%s] requesting server status: %s\n", m.Author.Username, m.Author.ID, tokens[1])
			srv, err := state.NewServer(tokens[1])
			if err != nil {
				log.Println(err)
				return
			}
			info, err := srv.FetchInfo()
			if err != nil {
				log.Println("serverinfo fetch fail:", err)
				return
			}
			status := FormatStatus(info)
			s.ChannelMessageSend(m.ChannelID, status)
		}()
	}
}

// handleMessageAttachments will inspect any file attachments to messages
// posted in the channels, decide if it's something it should handle (maps),
// download and do something with them.
func handleMessageAttachments(s *discordgo.Session, m *discordgo.MessageCreate) {
	if Contains(m.ChannelID, config.GetMapChannels()) {
		go func() {
			pm, err := s.UserChannelCreate(m.Author.ID)
			if err != nil {
				log.Println("error creating direct message channel:", err)
			}
			for _, v := range m.Attachments {
				dl, err := url.Parse(v.URL)
				if err != nil {
					log.Printf("unable to parse %q as a url: %v\n", v.URL, err)
					continue
				}
				extension := validFileExtension(dl.Path, fileTypes)
				if extension == "" {
					continue
				}
				data, err := grabFileContents(v.URL)
				if err != nil {
					log.Printf("error downloading %v: %v\n", v.URL, err)
					continue
				}
				name := uuid.New().String()
				dest := path.Join(config.TempPath, name)
				err = os.WriteFile(dest, data, 0644)
				if err != nil {
					log.Printf("error writing %q: %v\n", dest, err)
					continue
				}
				log.Printf("saving %q to %q\n", v.URL, dest)

				switch extension {
				case ".bsp":
					processBSP(dest, s, pm)
				case ".pak":
					processPAK(dest, s, m)
				}
				//s.ChannelMessageSend(dm.ID, "Adding map "+filename)
				//log.Printf("%s[%s] added map %s\n", m.Author.Username, m.Author.ID, filename)
			}
		}()
	}
}

func processBSP(mapfile string, s *discordgo.Session, pm *discordgo.Channel) {
	var msg string
	bspfile, err := bsp.OpenBSPFile(mapfile)
	if err != nil {
		msg = fmt.Sprintf("invalid BPS file: %v\n", err)
		log.Println(msg)
		s.ChannelMessageSend(pm.ID, msg)
		return
	}
	msg = fmt.Sprintf("Adding BSP file\n%d entities\n%d textures\n", len(bspfile.Ents), len(bspfile.FetchTextures()))
	s.ChannelMessageSend(pm.ID, msg)
}

// If we're given a .pak file to add, it must contain the proper virtual file
// system structure. It should contain a `/maps` folder containing any .bps
// files, a `/textures` directory for textures, etc. Random files in the root
// will not be committed.
func processPAK(filename string, s *discordgo.Session, m *discordgo.MessageCreate) {
	pm, err := s.UserChannelCreate(m.Author.ID)
	if err != nil {
		log.Println("error creating direct message channel:", err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("error reading pak data: %v\n", err)
		msg := "sorry, I was unable to process " + filename
		s.ChannelMessageSend(pm.ID, msg)
		return
	}
	pakfile, err := pak.Unmarshal(data)
	if err != nil {
		log.Printf("error unmarshalling pak data: %v\n", err)
		msg := fmt.Sprintf("%q invalid pak file", filename)
		s.ChannelMessageSend(pm.ID, msg)
		return
	}
	filesAdded := 0
	for _, f := range pakfile.GetFiles() {
		if hasPrefix(f.GetName(), []string{"maps/", "models/", "textures/", "env/", "sounds/", "pics/", "players/"}) {
			err = writePakFileToRepo(f)
			if err != nil {
				log.Println(err)
				continue
			}
			filesAdded++
		}
	}
	if filesAdded > 0 {
		git := NewGit(config.RepoPath)
		err := git.add()
		if err != nil {
			log.Println(err)
			return
		}
		err = git.commit(pm.Name)
		if err != nil {
			log.Println(err)
			return
		}
		err = git.Push()
		if err != nil {
			log.Println(err)
			return
		}
		msg := fmt.Sprintf("Files in `%s` have been committed to the our git repo", filename)
		s.ChannelMessageSend(pm.ID, msg)
		log.Printf("%q committed to git repo", filename)
	}
}

func writePakFileToRepo(f *proto.PAKFile) error {
	fullpath := path.Join(config.GetRepoPath(), f.Name)
	err := os.MkdirAll(filepath.Dir(fullpath), 0755)
	if err != nil {
		return fmt.Errorf("error creating full path: %v", err)
	}
	err = os.WriteFile(fullpath, f.Data, 0644)
	if err != nil {
		return fmt.Errorf("error writing %q to repo: %v", fullpath, err)
	}
	return nil
}

// custom version of strings.HasPrefix() to check against a slice of possbile
// prefixes. If any of them match, it returns true.
func hasPrefix(filename string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(filename, p) {
			return true
		}
	}
	return false
}

func validFileExtension(dl string, exts []string) string {
	out := ""
	for _, t := range exts {
		if strings.HasSuffix(dl, t) {
			out = t
		}
	}
	return out
}

func grabFileContents(url string) ([]byte, error) {
	out := []byte{}
	r, err := http.Get(url)
	if err != nil {
		return out, err
	}
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return out, err
	}
	return data, nil
}

// Helper function, if string is in slice
//
// Can use "all" in slice to match any
// Can use "-something" in conjunction with "all" for an exception
func Contains(needle string, haystack []string) bool {
	yes := false
	for i := range haystack {
		if haystack[i] == "all" {
			yes = true
		}
		if string(haystack[i][0]) == "-" && (haystack[i])[1:] == needle {
			yes = false
		}
		if string(haystack[i][0]) == "+" && (haystack[i])[1:] == needle {
			yes = true
		}
		if needle == haystack[i] {
			yes = true
		}
	}
	return yes
}

// Format the ServerInfo output for printing
func FormatStatus(info state.ServerInfo) string {
	output := fmt.Sprintf(
		"%s\n%s - %s/%s",
		info.Server["hostname"],
		info.Server["mapname"],
		info.Server["player_count"],
		info.Server["maxclients"],
	)
	if info.Server["gamedir"] == "opentdm" {
		if info.Server["time_remaining"] != "WARMUP" {
			output = fmt.Sprintf(
				"%s\nMatch time remaining: %s\nScore: %s:%s",
				output,
				info.Server["time_remaining"],
				info.Server["score_a"],
				info.Server["score_b"],
			)
		}
	}
	if len(info.Players) > 0 {
		var players []string
		for _, p := range info.Players {
			players = append(players, p.Name)
		}
		output += fmt.Sprintf("\n[`%s`]", strings.Join(players, ", "))
	}
	return output
}
