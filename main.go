package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

var (
	Token    string
	Channels string
	SavePath string
	Chans    []string
	STDOut   bool
)

func init() {
	f, err := os.OpenFile("bot.log", os.O_WRONLY | os.O_CREATE | os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}

	flag.StringVar(&Token, "t", "", "Bot Token")
	flag.StringVar(&Channels, "c", "", "The channels to listen in")
	flag.StringVar(&SavePath, "p", ".", "Path to save maps prior to syncing") 
	flag.BoolVar(&STDOut, "stdout", false, "Log to STDOUT instead of the logfile")
	flag.Parse()

	if !STDOut {
		log.SetOutput(f)
	} else {
		f.Close()
	}

	if Token == "" {
		flag.Usage()
		log.Panic("")
	}

	Chans = strings.Split(Channels, ",")
}

func main() {

	bot, err := discordgo.New("Bot " + Token)
	if err != nil {
		log.Println("error creating Discord session,", err)
		return
	}

	bot.AddHandler(messageCreate)

	// we only care about receiving message events.
	bot.Identify.Intents = discordgo.IntentsGuildMessages

	// Open a websocket connection to Discord and begin listening.
	err = bot.Open()
	if err != nil {
		log.Println("error opening connection,", err)
		return
	}

	log.Printf("PF Discord Bot Running...\n")
	// Wait here until CTRL-C or other term signal is received.
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

	if strings.HasPrefix(m.Content, "!q2 ") {
		go func() {
			parts := strings.Split(m.Content, " ")
			if len(parts) < 2 {
				return
			}

			q2server := parts[1]
			status := ServerStatus(q2server)
			s.ChannelMessageSend(m.ChannelID, status)
			log.Printf("%s[%s] requesting server status: %s\n", m.Author.Username, m.Author.ID, q2server)
		}()
		return
	}

	// only process attachments in designated channels
	if Contains(m.ChannelID, Chans) {
		if len(m.Attachments) > 0 {
			go func() {
				for _, v := range m.Attachments {
					filename, valid := ValidateFile(strings.ToLower(v.URL))
					if !valid {
						s.ChannelMessageSend(m.ChannelID, "Invalid start, `*.bsp` only please")
						return
					}

					DownloadFile(SavePath + "/" + filename, v.URL)
					s.ChannelMessageSend(m.ChannelID, "Adding map " + filename)

					log.Printf("%s[%s] added map %s\n", m.Author.Username, m.Author.ID, filename)
				}

				s.ChannelMessageSend(m.ChannelID, "Syncing...(usually takes 60s or so)")
				log.Println("Syncing")
				SyncWithServers()
				s.ChannelMessageSend(m.ChannelID, "finished.")
				log.Println("Sync finished")
			}()
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
	//validexts := []string{"bsp","zip","pak","pkz"}
	validexts := []string{"bsp"} // for now

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

//
// Fetch server status
//
func ServerStatus(q2server string) string {
	p := make([]byte, 1500)

	// only use IPv4
	conn, err := net.Dial("udp4", q2server)
	if err != nil {
		log.Printf("Connection error %v\n", err)
		return q2server + " - Connection error"
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))

	cmd := []byte{0xff, 0xff, 0xff, 0xff}
	cmd = append(cmd, "status"...)
	fmt.Fprintln(conn, string(cmd))

	_, err = bufio.NewReader(conn).Read(p)
	if err != nil {
		log.Println("Read error:", err)
		return "Read error"
	}

	lines := strings.Split(strings.Trim(string(p), " \n\t"), "\n")
	serverinfo := lines[1][1:]
	playerinfo := lines[2 : len(lines)-1]

	info := map[string]string{}
	vars := strings.Split(serverinfo, "\\")

	for i := 0; i < len(vars); i += 2 {
		info[strings.ToLower(vars[i])] = vars[i+1]
	}

	playercount := len(playerinfo)
	info["player_count"] = fmt.Sprintf("%d", playercount)

	if playercount > 0 {
		players := ""

		for _, p := range playerinfo {
			player := strings.SplitN(p, " ", 3)
			players = fmt.Sprintf("%s,%s", players, player[2])
		}

		info["players"] = players[1:]
	}

	output := fmt.Sprintf(
		"%s\n%s - %s/%s",
		info["hostname"],
		info["mapname"],
		info["player_count"],
		info["maxclients"])

	if info["gamedir"] == "opentdm" {
		if info["time_remaining"] != "WARMUP" {
			output = fmt.Sprintf(
				"%s\nMatch time remaining: %s\nScore: %s:%s",
				output,
				info["time_remaining"],
				info["score_a"],
				info["score_b"])
		}
	}

	pcount, _ := strconv.Atoi(info["player_count"])

	if pcount > 0 {
		players_array := strings.Split((info["players"])[1:len(info["players"])], "\",\"")
		players := ""
		for _, p := range players_array {
			players = players + p + ", "
		}
		output = fmt.Sprintf("%s\n[`%s`]", output, players[:len(players)-3])
	}

	return output
}

func SyncWithServers() {
	_, err := exec.Command("./push.sh").Output()
	if err != nil {
		log.Println(err)
	}
}

